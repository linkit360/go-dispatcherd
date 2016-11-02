.PHONY: dev run build seddev sendprod

dev:
	rm dispatcherd; go build; ./dispatcherd;

run:
	./dispatcherd

build:
	rm -v bin/dispatcherd-linux-amd64; \
	export GOOS=linux; export GOARCH=amd64; \
  go build -ldflags "-s -w" -o bin/dispatcherd-linux-amd64

sendprod:
	ssh -i ~/.ssh/mobilink_centos.pem centos@52.66.23.201 rm /home/centos/go/src/github.com/vostrok/dispatcherd/bin/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos.pem bin/dispatcherd-linux-amd64 centos@52.66.23.201:/home/centos/go/src/github.com/vostrok/dispatcherd/bin/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos.pem dev/appconfig.yml centos@52.66.23.201:/home/centos/go/src/github.com/vostrok/dispatcherd/dev/

senddev:
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem centos@35.154.8.158 rm /home/centos/go/src/github.com/vostrok/dispatcherd/bin/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem bin/dispatcherd-linux-amd64 centos@35.154.8.158:/home/centos/go/src/github.com/vostrok/dispatcherd/bin/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem  dev/appconfig.ymlcentos@35.154.8.158:/home/centos/go/src/github.com/vostrok/dispatcherd/dev/

access:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://52.66.23.201/mobilink-p2" 

agree:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://52.66.23.201/campaign/f90f2aca5c640289d0a29417bcb63a37"

testlocal:
	curl -i -L --header "HTTP_MSISDN: 928974412092" --header "X-Real-Ip: 10.80.128.1" --header "Host: pk.linkit360.ru" "http://localhost:50300/mobilink-p2"

testlocal1:
	curl -i -L --header "X-Real-Ip: 10.80.128.1" --header "Host: pk.linkit360.ru" "http://localhost:50300/mobilink-p2?msisdn=928974412092"

agreelocal:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://localhost:50300/campaign/f90f2aca5c640289d0a29417bcb63a37"

cqrcampaign:
	curl http://localhost:50300/cqr?t=campaigns

cqroperator_ip:
	curl http://localhost:50300/cqr?t=operator_ip
