build:
	go build -v

clean:
	rm go.mod 
	rm go.sum 
	go mod init 
	go build -v

release:
	rm -f *.zip
	rm -f miti 
	rm -rf dist 
	go generate
	git push
	goreleaser release

linux:
	go build -v -o miti
	zip miti_linux_amd64.zip miti README.md LICENSE 
	.github/uploadrelease.sh miti_linux_amd64.zip


arm:
	go build -v -o miti
	zip miti_linux_arm.zip miti README.md LICENSE 
	.github/uploadrelease.sh miti_linux_arm.zip

win:
	# $env:CGO_CFLAGS="-IC:\msys64\mingw64\include"
	# $env:CGO_LDFLAGS="-LC:\msys64\mingw64\lib"
	go build -v
	cp C:\\msys64\\mingw64\\bin\\libportmidi.dll .
	rm -f miti_windows.zip
	zip miti_windows.zip miti.exe README.md LICENSE libportmidi.dll
	
