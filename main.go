package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/sprig"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoprint"
	"github.com/jhump/protoreflect/grpcreflect"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	"text/template"
)

const basedir = "tmp"

func main() {
	url := os.Getenv("GRPC_URL")
	log.Infof("Processing url=%v target=%v", url, basedir)

	conn, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		log.WithError(err).Fatal("Error connecting to client")
	}
	defer conn.Close()

	stub := rpb.NewServerReflectionClient(conn)
	client := grpcreflect.NewClient(context.Background(), stub)
	services := listServices(client)
	protoFiles := listProtos(client, services)
	compileProtoFiles(protoFiles)
	generateGateway(services, protoFiles)
}

func listServices(client *grpcreflect.Client) []*desc.ServiceDescriptor {
	serviceNames, err := client.ListServices()
	if err != nil {
		log.WithError(err).Fatal("Error listing services through gRPC reflect API")
	}
	services := make([]*desc.ServiceDescriptor, len(serviceNames))
	for i, serviceName := range serviceNames {
		service, err := client.ResolveService(serviceName)
		if err != nil {
			log.WithError(err).Fatalf("Error resolving service %s", serviceName)
		}
		services[i] = service
	}
	return services
}

func listProtos(client *grpcreflect.Client, services []*desc.ServiceDescriptor) map[string]*desc.FileDescriptor {
	protoFiles := map[string]*desc.FileDescriptor{}
	for _, service := range services {
		protoFiles[service.GetFile().GetName()] = service.GetFile()
		for _, method := range service.GetMethods() {
			protoFiles[method.GetFile().GetName()] = method.GetFile()
			if method.GetInputType() != nil {
				protoFiles = mergeProtoMap(protoFiles, traverseMessage(method.GetInputType()))
			}
			if method.GetOutputType() != nil {
				protoFiles = mergeProtoMap(protoFiles, traverseMessage(method.GetOutputType()))
			}
		}
	}
	return protoFiles
}

func traverseMessage(message *desc.MessageDescriptor) map[string]*desc.FileDescriptor {
	protoFiles := map[string]*desc.FileDescriptor{}
	protoFiles[message.GetFile().GetName()] = message.GetFile()
	for _, field := range message.GetFields() {
		protoFiles = mergeProtoMap(protoFiles, traverseField(field))
	}
	return protoFiles
}

func traverseField(field *desc.FieldDescriptor) map[string]*desc.FileDescriptor {
	protoFiles := map[string]*desc.FileDescriptor{}
	if field.GetEnumType() != nil {
		protoFiles[field.GetEnumType().GetFile().GetName()] = field.GetEnumType().GetFile()
	}
	if field.GetMessageType() != nil {
		protoFiles = mergeProtoMap(protoFiles, traverseMessage(field.GetMessageType()))
	}
	if field.GetMapKeyType() != nil {
		protoFiles = mergeProtoMap(protoFiles, traverseField(field.GetMapKeyType()))
	}
	if field.GetMapValueType() != nil {
		protoFiles = mergeProtoMap(protoFiles, traverseField(field.GetMapValueType()))
	}
	return protoFiles
}

func mergeProtoMap(protoMaps ...map[string]*desc.FileDescriptor) map[string]*desc.FileDescriptor {
	target := map[string]*desc.FileDescriptor{}
	for _, protoMap := range protoMaps {
		for key, value := range protoMap {
			target[key] = value
		}
	}
	return target
}

func compileProtoFiles(protoFiles map[string]*desc.FileDescriptor) {
	if err := os.RemoveAll(basedir); err != nil {
		log.WithError(err).Fatalf("Error removing temp directory %s", basedir)
	}
	if err := os.MkdirAll(basedir, 0755); err != nil {
		log.WithError(err).Fatalf("Error creating temp directory %s", basedir)
	}
	for _, protoFile := range protoFiles {
		writeProtoFile(protoFile)
	}

	protoFileNames := make([]string, 0, len(protoFiles))
	for k := range protoFiles {
		protoFileNames = append(protoFileNames, k)
	}

	log.Infof("Compiling protobufs")
	cmd := exec.Command("protoc",
		"-I.",
		"-I$GOPATH/src",
		fmt.Sprintf("--go_out=plugins=grpc:."),
		protoFileNames[0])
	cmd.Dir = basedir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.WithError(err).Fatal("Error compiling protobufs")
	}
}

func writeProtoFile(protoFile *desc.FileDescriptor) {
	protoFilePath := filepath.Join(basedir, protoFile.GetName())
	if err := os.MkdirAll(filepath.Dir(protoFilePath), 0755); err != nil {
		log.WithError(err).Fatalf("Error creating proto directory %s", protoFilePath)
	}
	outFile, err := os.OpenFile(protoFilePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.WithError(err).Fatalf("Error creating proto file %s", protoFilePath)
	}
	defer outFile.Close()
	log.Infof("Writing file %v", protoFilePath)
	printer := protoprint.Printer{}
	printer.PrintProtoFile(protoFile, outFile)
}

func generateGateway(services []*desc.ServiceDescriptor, protoFiles map[string]*desc.FileDescriptor) {
	serviceProtos := make(map[string]*desc.FileDescriptor)
	for _, service := range services {
		if strings.Contains(service.GetName(), "ServerReflection") {
			continue
		}
		protoPath := service.GetFile().GetName()
		serviceProtos[protoPath] = protoFiles[protoPath]
	}

	for path, proto := range serviceProtos {
		compileGateway(proto, path)
	}
}

func compileGateway(protoFile *desc.FileDescriptor, protoPath string) {
	gwConfigPath := protoPath + ".gw.yaml"
	gwConfigFile, err := os.OpenFile(filepath.Join(basedir, gwConfigPath), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.WithError(err).Fatalf("Error creating gateway config %s", gwConfigPath)
	}
	log.Infof("Writing config file %v", gwConfigPath)
	gwTemplate := template.Must(template.New("gateway.yaml.tpl").Funcs(sprig.TxtFuncMap()).ParseGlob("templates/*.tpl"))
	err = gwTemplate.Execute(gwConfigFile, protoFile)
	if err != nil {
		log.WithError(err).Fatalf("Error rendering gateway config %s", gwConfigPath)
	}
	gwConfigFile.Close()

	log.Infof("Creating gateway file %v", gwConfigPath)
	cmd := exec.Command("protoc",
		"-I.",
		"-I$GOPATH/src",
		"-I$GOPATH/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.4.1/third_party/googleapis",
		fmt.Sprintf("--swagger_out=logtostderr=true,grpc_api_configuration=%s,v=2:.", gwConfigPath),
		fmt.Sprintf("--grpc-gateway_out=logtostderr=true,grpc_api_configuration=%s,v=2:.", gwConfigPath),
		protoPath)
	cmd.Dir = basedir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.WithError(err).Fatalf("Error generating gateway file for proto %s, config %s", protoPath, gwConfigPath)
	}

	gwPackage := strings.Replace(protoPath, ".proto", "", -1)
	if err := os.MkdirAll(filepath.Join(basedir, gwPackage), 0755); err != nil {
		log.WithError(err).Fatalf("Error creating package %s", gwPackage)
	}
	if err := os.Rename(filepath.Join(basedir, gwPackage+".pb.go"), filepath.Join(basedir, gwPackage, "proto.pb.go")); err != nil {
		log.WithError(err).Fatalf("Error moving protobuf %s", gwPackage+".pb.go")
	}
	if err := os.Rename(filepath.Join(basedir, gwPackage+".pb.gw.go"), filepath.Join(basedir, gwPackage, "gateway.pb.gw.go")); err != nil {
		log.WithError(err).Fatalf("Error moving gateway %s", gwPackage+".pb.go")
	}
	if err := os.Rename(filepath.Join(basedir, gwPackage+".swagger.json"), filepath.Join(basedir, gwPackage, gwPackage+".swagger.json")); err != nil {
		log.WithError(err).Fatalf("Error moving swagger %s", gwPackage+".pb.go")
	}
}
