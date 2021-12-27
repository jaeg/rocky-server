# Rocky Server

### Command Line Options
-  -cert-file string
        location of cert file
-  -communication-port string
        Port that is used for individual proxying requests (default ":9998")
-  -key-file string
        location of key file
-  -log-path string
        Logs location (default "./logs.txt")
-  -proxy-port string
        Port real clients connect to IE the exposed port (default ":8099")
-  -server-port string
        Port rocky clients connect to for management (default ":9999")

### How to Run
#### Option 1: From source
- `make vendor`
- `make run`

#### Option 2: Build it
- `make vendor`
- `make build` - will build for current system architecture. 
- `make build-linux` - will build Linux distributable
- `make build-pi` - will build Raspberry Pi compatible distributable
- You will find the executable in the `./bin` folder.

#### Option 3: Docker
Linux images:
- `docker run -d jaeg/rocky-server:latest`

Raspberry pi images:
- `docker run -d jaeg/rocky-server:latest-pi`
