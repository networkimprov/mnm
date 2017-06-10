package main

import (
   "time"
   "fmt"
   "net"
   "qlib"
)

var sId chan int

func main() {
   fmt.Printf("Starting Test Pass\n")
   sId = make(chan int, 10)
   sId <- 111111
   sId <- 222222
   qlib.Init("qstore")
   for {
      qlib.NewLink(NewTc(<-sId))
   }
}

func NewTc(a int) *tTestClient {
   return &tTestClient{id:a, to:a+111111, ack:make(chan int,10)}
}

type tTestClient struct {
   id, to, count int
   ack chan int
   closed bool
}

func (o *tTestClient) Read(buf []byte) (int, error) {
   if o.count % 10 == 9 {
      time.AfterFunc(2*time.Second, func(){ sId <- o.id })
      fmt.Printf("%d testclient.read preparing to log out\n", o.id)
      return 0, &net.OpError{Op:"log out"}
   }
   aTmr := time.NewTimer(10 * time.Millisecond)
   var aS string
   select {
   case <-o.ack:
      aS = fmt.Sprintf(`{"Type":"ack"}`)
      aTmr.Stop()
   case <-aTmr.C:
      o.count++
      if o.count == 1 {
         aS = fmt.Sprintf(`{"Type":"login", "Id":"%d"}`, o.id)
      } else {
         aS = fmt.Sprintf(`{"Type":"text",  "To":"%d", "Data":"msg %d"}`, o.to, o.count)
      }
   }
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

func (o *tTestClient) Close() error { o.closed = true; return nil }
func (o *tTestClient) LocalAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) RemoteAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) SetDeadline(time.Time) error { return nil }
func (o *tTestClient) SetReadDeadline(time.Time) error { return nil }
func (o *tTestClient) SetWriteDeadline(time.Time) error { return nil }

