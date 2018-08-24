package:
	docker build -t grpc-rest-sidecar:latest .

shell: package
	docker run -it --rm grpc-rest-sidecar:latest bash
