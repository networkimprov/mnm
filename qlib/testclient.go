package qlib

import (
   "fmt"
   "encoding/json"
   "net"
   "strconv"
   "time"
)


var sTestClientId chan int

type tTestClient struct {
   id, to int // who i am, who i send to
   count int // msg number
   deferLogin bool // test login timeout feature
   ack chan string // writer tells reader to issue ack to qlib
   closed bool // when about to shut down
   readDeadline time.Time // set by qlib
}

func InitTestClient(i int) {
   sTestClientId = make(chan int, i)
   for a:=1; a <= i; a++ {
      sTestClientId <- 111111 * a
   }
}

func NewTestClient(iDawdle bool) *tTestClient {
   a := <-sTestClientId
   return &tTestClient{
      id: a, to: a+111111,
      deferLogin: iDawdle,
      ack: make(chan string, 10),
   }
}

func (o *tTestClient) Read(buf []byte) (int, error) {
   if o.count % 20 == 19 {
      return 0, &net.OpError{Op:"read", Err:tTestClientError("log out")}
   }

   var aDlC <-chan time.Time
   if !o.readDeadline.IsZero() {
      aDl := time.NewTimer(o.readDeadline.Sub(time.Now()))
      defer aDl.Stop()
      aDlC = aDl.C
   }

   aUnit := 200 * time.Millisecond; if o.deferLogin { aUnit = 6 * time.Second }
   aTmr := time.NewTimer(aUnit)
   defer aTmr.Stop()

   var aHead map[string]interface{}
   var aData string

   select {
   case aId := <-o.ack:
      aHead = tMsg{"Op":eAck, "Id":aId, "Type":"n"}
   case <-aTmr.C:
      o.count++
      if o.deferLogin {
         aHead = tMsg{}
      } else if o.count == 1 {
         aHead = tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "NodeId":fmt.Sprint(o.id)}
      } else if o.id == 222222 && o.count % 20 == 2 {
         aHead = tMsg{"Op":ePing, "Id":fmt.Sprint(o.count), "From":"a2", "To":"a1"}
      } else {
         aFor := tHeaderFor{Id:"u"+fmt.Sprint(o.to), Type:eForUser}
         if o.count % 20 >= 18 { aFor = tHeaderFor{Id:"g1", Type:eForGroupAll} }
         if o.count % 20 == 19 { aFor.Type = eForGroupExcl }
         aHead = tMsg{"Op":ePost, "Id":fmt.Sprint(o.count), "For":[]tHeaderFor{aFor}}
         aData = fmt.Sprintf(" |msg %d|", o.count)
      }
   case <-aDlC:
      return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
   }

   aMsg := PackMsg(aHead, []byte(aData))
   fmt.Printf("%d testclient.read %s\n", o.id, string(aMsg))
   return copy(buf, aMsg), nil
}

func (o *tTestClient) Write(iBuf []byte) (int, error) {
   if o.closed {
      fmt.Printf("%d testclient.write was closed\n", o.id)
      return 0, &net.OpError{Op:"write", Err:tTestClientError("closed")}
   }
   fmt.Printf("%d testclient.write got %s\n", o.id, string(iBuf))

   aHeadLen,_ := strconv.ParseInt(string(iBuf[:4]), 16, 0)
   aHead := &tClientHead{}
   err := json.Unmarshal(iBuf[4:aHeadLen+4], aHead)
   if err != nil { panic(err) }

   if aHead.Op == "delivery" {
      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.ack <- aHead.Id:
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Printf("%d testclient.write timed out on ack\n", o.id)
         return 0, &net.OpError{Op:"write", Err:tTestClientError("noack")}
      }
   }

   return len(iBuf), nil
}

func (o *tTestClient) SetReadDeadline(i time.Time) error {
   o.readDeadline = i
   return nil
}

func (o *tTestClient) Close() error {
   o.closed = true;
   time.AfterFunc(10*time.Millisecond, func(){ sTestClientId <- o.id })
   return nil
}

func (o *tTestClient) LocalAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) RemoteAddr() net.Addr { return &net.UnixAddr{"e", "a"} }
func (o *tTestClient) SetDeadline(time.Time) error { return nil }
func (o *tTestClient) SetWriteDeadline(time.Time) error { return nil }

type tClientHead struct { Op, Info, Id, From string }

type tTimeoutError struct{}
func (o *tTimeoutError) Error() string   { return "i/o timeout" }
func (o *tTimeoutError) Timeout() bool   { return true }
func (o *tTimeoutError) Temporary() bool { return true }

type tTestClientError string
func (o tTestClientError) Error() string { return string(o) }


