.PHONY: dev run build seddev sendprod

dev:
	rm dispatcherd; go build; export PATH="web/" ; ./dispatcherd;

run:
	export PATH="web/" ; ./dispatcherd

rm:
    rm ~/linkit/dispatcherd-linux-amd64; rm -v bin/dispatcherd-linux-amd64;

build:
	export GOOS=linux; export GOARCH=amd64; \
  go build -ldflags "-s -w" -o bin/dispatcherd-linux-amd64 ; \
  cp  bin/dispatcherd-linux-amd64 ~/linkit

senddev:
	ssh -i ~/.ssh/mobilink_centos_testing_stage.pem centos@35.154.8.158 rm /home/centos/linkit/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem bin/dispatcherd-linux-amd64 centos@35.154.8.158:/home/centos/linkit/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem dev/dispatcherd.yml centos@35.154.8.158:/home/centos/linkit/
	ssh -i ~/.ssh/mobilink_centos_testing_stage.pem centos@35.154.8.158 sudo supervisorctl dispatcherd restart 

config:
	scp -i ~/.ssh/mobilink_centos_testing_stage.pem dev/dispatcherd.yml centos@35.154.8.158:/home/centos/linkit/
	
sendprod:
	ssh -i ~/.ssh/mobilink_centos.pem centos@52.66.23.201 rm /home/centos/linkit/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos.pem bin/dispatcherd-linux-amd64 centos@52.66.23.201:/home/centos/linkit/dispatcherd-linux-amd64
	scp -i ~/.ssh/mobilink_centos.pem dev/dispatcherd.prod.yml centos@52.66.23.201:/home/centos/linkit/

access:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://35.154.8.158/mobilink-p2" 

agree:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://35.154.8.158/campaign/f90f2aca5c640289d0a29417bcb63a37?aff_sub=hIDMA1511170000000001035050071575WF0TPC79c000723PZ02345"

testlocal:
	curl -i -L --header "HTTP_MSISDN: 928974412092" --header "X-Real-Ip: 10.80.128.1" --header "Host: pk.linkit360.ru" "http://localhost:50300/mobilink-p2"

testlocal1:
	curl -i -L --header "X-Real-Ip: 10.80.128.1" --header "Host: pk.linkit360.ru" "http://localhost:50300/mobilink-p2?msisdn=928974412092"

pixel:
	curl -L "http://35.154.8.158/mobilink-p2?msisdn=928777777777&aff_sub=hIDMA1511170000000001035050071575WF0TPC79c000723PZ02345"

pixellocal:
	curl -L --header "X-Real-Ip: 10.80.128.1" "http://localhost:50300/mobilink-p2?msisdn=928777777777&aff_sub=hIDMA1511170000000001035050071575WF0TPC79c000723PZ02345"

agreelocal:
	curl -L -H 'HTTP_MSISDN: 928974412092' -H 'X-Real-Ip: 10.80.128.1' -H 'Host: pk.linkit360.ru' "http://localhost:50300/campaign/f90f2aca5c640289d0a29417bcb63a37?aff_sub=hIDMA1511170000000001035050071575WF0TPC79c000723PZ02345"

cqrcampaign:
	curl http://localhost:50300/cqr?t=campaigns

cqroperator_ip:
	curl http://localhost:50300/cqr?t=operator_ip
