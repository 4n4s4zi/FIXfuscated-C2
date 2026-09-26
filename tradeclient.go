package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "strings"

    "fix/fix"
)

func main() {
    //Initialize TCP connection to executor
    if len(os.Args) < 3 {
        fmt.Fprintf(os.Stderr, "Usage: %s <ip> <port>\n", os.Args[0])
        os.Exit(1)
    }
    srvAddr := net.JoinHostPort(os.Args[1], os.Args[2])

    conn, err := net.Dial("tcp", srvAddr)
    if err != nil {
        fmt.Printf("Failed to connect: %v\n", err)
        os.Exit(1)
    }
    defer conn.Close()
    fmt.Println("Connected to server.")

    //Configure FIX session settings
    s := fix.New(conn, fix.Config{
        BeginString:  "FIX.4.4",
        SenderCompID: "123",
        TargetCompID: "456",
        //HeartBtInt:   30,
    })

    //Send FIX LOGON message
    if err := s.Logon(); err != nil {
        fmt.Println(err)
        fmt.Println("FIX logon error.")
        os.Exit(1)
    }

    //Listen for Logon confirmation
    fmt.Println("FIX logon confirmed.")

    //Send order "knock" for auth
    s.SendTradeOrder("MCD", 333, nil)
    //fmt.Println("Order knock sent")

    hostname, err := s.RecvExecReport()
    if err != nil {
        fmt.Println(err)
        fmt.Println("Error receiving knock confirmation.")
        os.Exit(1)
    }
    fmt.Println("Knock confirmation received.")
    fmt.Printf("Connected to host: %s\n*\n*\n*\n", string(hostname))

    //Begin interactive command loop
    sc := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("$ ")
        if !sc.Scan() {
            break
        }
        line := strings.TrimSpace(sc.Text())
        //fmt.Printf("line: %s\n", line)
        if line == "" {
            continue
        }
        if line == "exit" {
            break
        }
        err := s.SendTradeOrder("MCD", 1, []byte(line+"\n"))
        if err != nil {
            fmt.Println(err)
            break
        }
        out, err := s.RecvExecReport()
        if err != nil {
            break
        }
        os.Stdout.Write(out)
        if len(out) == 0 || out[len(out)-1] != '\n' {
            fmt.Println()
        }
    }
    if err := s.Close(); err != nil {
        fmt.Println(err)
    }
    fmt.Println("Logout sent.")
}
