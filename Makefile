.PHONY: all server cli plugins clean test

all: server cli

server:
	cd lolit-server && go build -o lolit-server .

cli:
	cd rv && go build -o rv .

plugins:
	@echo "SolidWorks Add-in: build in Visual Studio with .NET Framework 4.8"
	@echo "KiCAD Plugin: cd kicad-plugin && pip install -e ."

test:
	cd lolit-server && go test ./...
	cd rv && go test ./...

clean:
	rm -f lolit-server/lolit-server rv/rv
