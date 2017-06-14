package main

import (
   "time"
   "fmt"
   "net"
   "qlib"
)

var sId chan int
var sTimeout error = &tTimeoutError{}

func main() {
   aJso := qlib.PackMsg(map[string]interface{}{
      "s": "stuff",
      "etcdata": []string{ "a", "b" },
      "o": map[string]interface{}{ "t": "trash" },
   }, []byte{'#'})
   fmt.Printf("packmsg: %v\n", string(aJso))

   fmt.Printf("Starting Test Pass\n")
   sId = make(chan int, 10)
   sId <- 111111
   sId <- 222222
   qlib.Init("qstore")
   for a := 0; true; a++ {
      aDawdle := a == 1
      qlib.NewLink(NewTc(<-sId, aDawdle))
   }
}

const (
   _ = iota
   eRegister // uid newnode aliases
   eAddNode  // uid nodeid newnode
   eLogin    // uid nodeid
   eListEdit // id to type member
   ePost     // id for
   ePing     // id alias
   eAck      // id type
)

func NewTc(i int, iNoLogin bool) *tTestClient {
   return &tTestClient{id:i, to:i+111111, noLogin:iNoLogin, ack:make(chan int,10)}
}

type tTestClient struct {
   id, to, count int
   noLogin bool
   ack chan int
   closed bool
   readDeadline time.Time
}

func (o *tTestClient) Read(buf []byte) (int, error) {
   if o.count % 10 == 9 {
      return 0, &net.OpError{Op:"log out"}
   }
   var aDlC <-chan time.Time
   if !o.readDeadline.IsZero() {
      aDl := time.NewTimer(o.readDeadline.Sub(time.Now()))
      defer aDl.Stop()
      aDlC = aDl.C
   }
   aUnit := 200 * time.Millisecond; if o.noLogin { aUnit = 6 * time.Second }
   aTmr := time.NewTimer(aUnit)
   defer aTmr.Stop()
   var aS, aData string
   select {
   case <-o.ack:
      aS = fmt.Sprintf(`{"Op":%d}`, eAck)
   case <-aTmr.C:
      o.count++
      if o.noLogin {
         aS = `{}`
      } else if o.count == 1 {
         aS = fmt.Sprintf(`{"Op":%d, "Uid":"%d"}`, eLogin, o.id)
      } else {
         aS = fmt.Sprintf(`{"Op":%d, "To":"%d"}`, ePost, o.to)
         aData = fmt.Sprintf(" |msg %d|", o.count)
      }
   case <-aDlC:
      return 0, &net.OpError{Op:"timeout",Err:sTimeout}
   }
   aS = fmt.Sprintf("%04x"+aS+aData, len(aS))
   fmt.Printf("%d testclient.read %s\n", o.id, aS)
   return copy(buf, aS), nil
}

func (o *tTestClient) Write(buf []byte) (int, error) {
   if o.closed {
      fmt.Printf("%d testclient.write was closed\n", o.id)
      return 0, &net.OpError{Op:"closed"}
   }
   aTmr := time.NewTimer(2 * time.Second)
   select {
   case o.ack <- 1:
      aTmr.Stop()
   case <-aTmr.C:
      fmt.Printf("%d testclient.write timed out on ack\n", o.id)
      return 0, &net.OpError{Op:"noack"}
   }
   fmt.Printf("%d testclient.write got %s\n", o.id, string(buf))
   return len(buf), nil
}

func (o *tTestClient) SetReadDeadline(i time.Time) error {
   o.readDeadline = i
   return nil
}

func (o *tTestClient) Close() error {
   o.closed = true;
   time.AfterFunc(10*time.Millisecond, func(){ sId <- o.id })
   return nil
}

func (o *tTestClient) LocalAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) RemoteAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) SetDeadline(time.Time) error { return nil }
func (o *tTestClient) SetWriteDeadline(time.Time) error { return nil }

type tTimeoutError struct{}
func (o *tTimeoutError) Error() string   { return "i/o timeout" }
func (o *tTimeoutError) Timeout() bool   { return true }
func (o *tTimeoutError) Temporary() bool { return true }
