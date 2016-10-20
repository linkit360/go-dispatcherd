.PHONY: dev run release

dev:
	rm dispatcherd; go build; ./dispatcherd;

run:
	./bin/dispatcherd

release:
	export GOOS=linux; export GOARCH=amd64; \
  go build -ldflags "-s -w" -o bin/dispatcherd-linux-amd64

send:
	scp -i ~/.ssh/mobilink_centos.pem bin/dispatcherd-linux-amd64 centos@52.66.23.201:/home/centos/go/src/github.com/vostrok/dispatcherd/bin/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos.pem dev/appconfig.yml centos@52.66.23.201:/home/centos/go/src/github.com/vostrok/dispatcherd/dev/
