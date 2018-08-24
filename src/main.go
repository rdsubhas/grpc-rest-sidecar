package main

import (
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/grpcreflect"
	"github.com/jhump/protoreflect/desc/protoprint"
	"google.golang.org/grpc"
	"golang.org/x/net/context"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	log "github.com/sirupsen/logrus"
	"os"
	"fmt"
	"path/filepath"
	"strings"
)

func main() {
	url := os.Getenv("GRPC_URL")
	basepath := "tmp"
	log.Infof("Processing url=%v target=%v", url, basepath)

	conn, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	stub := rpb.NewServerReflectionClient(conn)
	client := grpcreflect.NewClient(context.Background(), stub)
	protoFiles := traverse(client)
	writeProtoFiles(protoFiles, basepath)
}

func traverse(client *grpcreflect.Client) map[string]*desc.FileDescriptor {
	protoFiles := map[string]*desc.FileDescriptor{}
	serviceNames, err := client.ListServices()
	if err != nil {
		panic(err)
	}

	for _, serviceName := range serviceNames {
		service, err := client.ResolveService(serviceName)
		if err != nil {
			panic(err)
		}

		protoFiles[service.GetFile().GetName()] = service.GetFile()
		for _, method := range service.GetMethods() {
			protoFiles[method.GetFile().GetName()] = method.GetFile()
			if (method.GetInputType() != nil) {
				protoFiles = mergeProtoMap(protoFiles, traverseMessage(method.GetInputType()))
			}
			if (method.GetOutputType() != nil) {
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
	if (field.GetEnumType() != nil) {
		protoFiles[field.GetEnumType().GetFile().GetName()] = field.GetEnumType().GetFile()
	}
	if (field.GetMessageType() != nil) {
		protoFiles = mergeProtoMap(protoFiles, traverseMessage(field.GetMessageType()))
	}
	if (field.GetMapKeyType() != nil) {
		protoFiles = mergeProtoMap(protoFiles, traverseField(field.GetMapKeyType()))
	}
	if (field.GetMapValueType() != nil) {
		protoFiles = mergeProtoMap(protoFiles, traverseField(field.GetMapValueType()))
	}
	return protoFiles
}

func mergeProtoMap(protoMaps... map[string]*desc.FileDescriptor) map[string]*desc.FileDescriptor {
	target := map[string]*desc.FileDescriptor{}
	for _, protoMap := range protoMaps {
		for key, value := range protoMap {
			if !strings.Contains(key, "reflection.proto") {
				target[key] = value
			}
		}
	}
	return target
}

func writeProtoFiles(protoFiles map[string]*desc.FileDescriptor, basepath string) {
	if err := os.RemoveAll(basepath); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(basepath, 0755); err != nil {
		panic(err)
	}
	for _, protoFile := range protoFiles {
		writeProtoFile(protoFile, basepath)
	}
	writeIndexFile(protoFiles, basepath)
	writeConfigFile(protoFiles, basepath)
}

func writeProtoFile(protoFile *desc.FileDescriptor, basepath string) {
	protoFilePath := filepath.Join(basepath, protoFile.GetName())
	if err := os.MkdirAll(filepath.Dir(protoFilePath), 0755); err != nil {
		panic(err)
	}
	outFile, err := os.OpenFile(protoFilePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer outFile.Close()
	log.Infof("Writing file %v", protoFilePath)
	printer := protoprint.Printer{}
	printer.PrintProtoFile(protoFile, outFile)
}

func writeIndexFile(protoFiles map[string]*desc.FileDescriptor, basepath string) {
	indexFilePath := filepath.Join(basepath, "proto.lst")
	indexFile, err := os.OpenFile(indexFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer indexFile.Close()
	log.Infof("Writing index file %v", indexFilePath)
	for protoFileName := range protoFiles {
		fmt.Fprintln(indexFile, protoFileName)
	}
}

func writeConfigFile(protoFiles map[string]*desc.FileDescriptor, basepath string) {
	configFilePath := filepath.Join(basepath, "proto.yaml")
	configFile, err := os.OpenFile(configFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer configFile.Close()
	log.Infof("Writing config file %v", configFilePath)

	fmt.Fprintf(configFile, `
type: google.api.Service
config_version: 3
http:
  rules:
`)

	for _, protoFile := range protoFiles {
		for _, service := range protoFile.GetServices() {
			for _, method := range service.GetMethods() {
				fmt.Fprintf(configFile, "    - selector: %v\n", method.GetFullyQualifiedName())
				fmt.Fprintf(configFile, "      post: /%v\n", strings.Replace(method.GetFullyQualifiedName(), ".", "/", -1))
				fmt.Fprintf(configFile, "      body: \"*\"\n")
			}
		}
	}
}
