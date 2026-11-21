# TursoDB Server

This project demonstrates how to run **TursoDB 0.8.4** as a local sync
server and access it from a Go application using the Hrana HTTP pipeline
endpoint.

The example uses:

-   TursoDB CLI/server binary: `tursodb`
-   A local SQLite-compatible database file: `production.db`
-   Sync server listening on port `8080`
-   Hrana endpoint: `POST /v2/pipeline`
-   Go client using only the Go standard library (`net/http`,
    `encoding/json`, etc.)

> **Scope:** These instructions document the working setup tested in the
> development environment. They do not assume that the sync server has
> authentication or TLS configured.

## 1. Prerequisites

On a new computer, install:

-   **WSL 2 with Ubuntu** (if using Windows), or a compatible Linux
    environment
-   **Go** (a recent stable release)
-   `curl`
-   `git` (optional, if cloning the project)

On Windows, install or enable WSL 2 and install an Ubuntu distribution.
Open the Ubuntu terminal for the Linux commands below.

Check the tools:

``` bash
go version
curl --version
```

## 2. Create the project directory

``` bash
mkdir -p ~/office/projects/labs/program/turso/turso_cli/turso-server/data
mkdir -p ~/office/projects/labs/program/turso/turso_cli/tursodb-go-client-example
cd ~/office/projects/labs/program/turso/turso_cli
```

The example directory layout will be:

``` text
turso_cli/
├── turso-server/
│   ├── tursodb
│   └── data/
│       └── production.db
└── tursodb-go-client-example/
    ├── go.mod
    └── main.go
```

## 3. Install or place the TursoDB binary

Place the **TursoDB 0.8.4** executable named `tursodb` in:

``` text
turso-server/tursodb
```

Use the appropriate official distribution or build instructions for your
operating system and CPU architecture. The binary used in the original
setup reports:

``` text
Turso 0.8.4
```

Make it executable and verify the version:

``` bash
chmod +x ./turso-server/tursodb
./turso-server/tursodb --version
```

If the version output differs, check compatibility before continuing.

## 4. Create the database

Create the data directory and database file:

``` bash
mkdir -p ./turso-server/data
./turso-server/tursodb ./turso-server/data/production.db
```

At the TursoDB SQL prompt, execute:

``` sql
CREATE TABLE IF NOT EXISTS server_info (
    id INTEGER PRIMARY KEY,
    hostname TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO server_info (hostname)
VALUES ('new-computer');

SELECT * FROM server_info;
```

Exit the SQL prompt using the shell's supported quit command (for
example, `.quit` if available).

The database file should now exist at:

``` text
./turso-server/data/production.db
```

## 5. Start the sync server

From the `turso_cli` directory, run:

``` bash
./turso-server/tursodb ./turso-server/data/production.db --sync-server 0.0.0.0:8080
```

Keep this terminal open while using the Go client.

The server listens on port `8080` on all network interfaces. To run it
in the background for a temporary development session, you can use:

``` bash
nohup ./turso-server/tursodb ./turso-server/data/production.db \
  --sync-server 0.0.0.0:8080 \
  > ./turso-server/server.log 2>&1 &
```

To find the process:

``` bash
pgrep -af tursodb
```

To stop a background instance, identify its PID and terminate that
process:

``` bash
kill <PID>
```

Do not start a second server against the same database file unless you
have confirmed that the configuration supports it.

## 6. Test the Hrana HTTP endpoint

In another terminal, run this request from the same Linux/WSL
environment:

``` bash
curl -i -X POST http://127.0.0.1:8080/v2/pipeline \
  -H "Content-Type: application/json" \
  -d '{
    "baton": null,
    "requests": [
      {
        "type": "execute",
        "stmt": {
          "sql": "SELECT * FROM server_info",
          "want_rows": true
        }
      }
    ]
  }'
```

A successful request returns **HTTP 200** and a JSON response containing
the selected row.

The root path (`/`) and a guessed database path are not health-check
endpoints in this setup. Use `/v2/pipeline` with a valid pipeline
request.

## 7. Configure the Go client

Go to the client directory:

``` bash
cd ~/office/projects/labs/program/turso/turso_cli/tursodb-go-client-example
```

If starting from a new project, initialize its module:

``` bash
go mod init turso-go-client-example
```

Place the client source file at `main.go`.

The client implementation in this example uses the Go standard library
and sends requests to:

``` text
http://127.0.0.1:8080/v2/pipeline
```

That address is correct when the client runs in the same WSL/Linux
environment as the server.

Build and run:

``` bash
go build -o turso-go-client .
./turso-go-client
```

The example client performs these operations:

1.  Selects rows from `server_info`.
2.  Inserts a row using `INSERT ... RETURNING`.
3.  Updates a row using `UPDATE ... RETURNING`.
4.  Selects the updated rows to verify the change.

The sample output should show the inserted row and its updated hostname.
Row IDs and timestamps will differ on each computer/run.

## 8. Connecting from Windows or another computer

### From Windows to a server running in WSL 2

Find the current WSL IP address:

``` bash
hostname -I
```

Use the appropriate IP address in the Go client's server URL, for
example:

``` text
http://<WSL-IP>:8080
```

The WSL IP can change after WSL restarts. If connectivity fails,
retrieve it again.

From Windows PowerShell, test the port:

``` powershell
Test-NetConnection -ComputerName <WSL-IP> -Port 8080
```

Check that `TcpTestSucceeded` is `True`.

### From another computer on the network

Use the host computer's reachable LAN address and ensure that:

-   The server is listening on the intended interface.
-   The host firewall allows inbound TCP traffic on port `8080`.
-   The client can route to the host address.

Do not expose this development server to an untrusted network or the
public internet.

## 9. Troubleshooting

### Connection refused

Confirm that the server is running:

``` bash
pgrep -af tursodb
```

Check whether port `8080` is listening:

``` bash
ss -lntp | grep ':8080'
```

### HTTP 404

Use the Hrana pipeline endpoint:

``` text
POST /v2/pipeline
```

A `GET /` request is not a valid test for this server setup.

### HTTP 500 or pipeline error

Check that the request includes both `baton` and `requests`, and that
the request type is supported. The tested request type is `execute`;
`close` was not accepted by the tested TursoDB version.

### The Go client cannot connect from Windows

Get the current WSL address again:

``` bash
hostname -I
```

Then test port `8080` from PowerShell using `Test-NetConnection`.

### `go build` reports module or source errors

Confirm that `main.go` is in the current directory and that the module
has been initialized:

``` bash
pwd
ls -la
cat go.mod
```

Then run:

``` bash
go build -o turso-go-client .
```

## 10. Important notes and current limitations

-   The working server command opens one database file directly and
    starts the sync server on port `8080`.
-   The Go client currently sends requests with `baton: null`. It does
    not yet implement persistent Hrana session/baton handling.
-   Do not assume that separate HTTP requests form one atomic
    transaction. Transaction support requires correct session/baton
    handling or a supported batch approach.
-   In the tested TursoDB 0.8.4 setup, the reported `affected_row_count`
    was `0` even when an INSERT or UPDATE succeeded. The example uses
    SQL `RETURNING` and `Query()` to retrieve the affected rows instead.
-   The example is a development setup, not a hardened production
    deployment.

## 11. Security

The server command binds to `0.0.0.0:8080`, and this example does not
configure authentication or TLS.

For local development, keep access limited to a trusted machine/network.
Before using this setup beyond a trusted development environment, put
appropriate network controls and authenticated, encrypted access in
place. Do not publish port `8080` directly to the internet.

## Quick start

After completing the initial setup, start the server:

``` bash
cd ~/office/projects/labs/program/turso/turso_cli

./turso-server/tursodb ./turso-server/data/production.db \
  --sync-server 0.0.0.0:8080
```

In a second terminal, run the Go client:

``` bash
cd ~/office/projects/labs/program/turso/turso_cli/tursodb-go-client-example

go build -o turso-go-client .
./turso-go-client
```
