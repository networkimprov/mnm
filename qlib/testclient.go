package qlib

import (
   "fmt"
   "encoding/json"
   "net"
   "strconv"
   "strings"
   "time"
)


const kTestLoginWait time.Duration = 6 * time.Second

var sTestClientId chan int
var sTestVerifyDone chan int
var sTestVerifyWant string // expected results of verifyRead()
var sTestVerifyGot [2]string // actual results of verifyRead()

func LocalTest(i int) {
   sTestVerifyDone = make(chan int)
   sTestVerifyWant = "\n"
   sTestVerifyGot[1] = "\n"

   //todo call UDb.AddUser(...)

   NewLink(newTestClient(eActVerifyRecv, 222223))
   NewLink(newTestClient(eActVerifySend, 111112))
   <-sTestVerifyDone
   time.Sleep(10 * time.Millisecond)
   fmt.Printf("Verify pass complete, starting cycle\n\n")

   sTestClientId = make(chan int, i)
   for a := 1; a <= i; a++ {
      sTestClientId <- 111111 * a
   }
   for a := 0; true; a++ {
      aAct := eActCycle
      if a == 1 { aAct = eActDeferLogin }
      NewLink(newTestClient(aAct, <-sTestClientId))
   }
}

type tTestClient struct {
   id, to int // who i am, who i send to
   count int // msg number
   action tTestAction // test mode
   work []tTestWork // verify sequence data
   ack chan string // writer tells reader to issue ack to qlib
   closed bool // when about to shut down
   readDeadline time.Time // set by qlib
}

type tTestAction int
const ( eActCycle tTestAction =iota; eActDeferLogin; eActVerifySend; eActVerifyRecv )

type tTestWork struct { msg []byte; head tMsg; data, want string }

func newTestClient(iAct tTestAction, iId int) *tTestClient {
   aTc := &tTestClient{
      id: iId, to: iId+111111,
      action: iAct,
      ack: make(chan string, 10),
   }
   if iAct == eActVerifySend {
      aTc.work = []tTestWork{
        { msg : []byte(`0007{"O":0}`) ,
          want: `0028{"info":"incomplete header","op":"quit"}` ,
      },{ msg : []byte(`00z1{"Op":3, "Uid":"noone"}`) ,
          want: `002c{"info":"invalid header length","op":"quit"}` ,
      },{ msg : []byte(`000a{"Op":12f3`) ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "NoId":"none"} ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "For":[]tHeaderFor{{}}} ,
          want: `003c{"info":"disallowed op on unauthenticated link","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "NodeId":fmt.Sprint(iId)} ,
          want: `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "NodeId":fmt.Sprint(iId)} ,
          want: `0036{"info":"disallowed op on connected link","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "NodeId":fmt.Sprint(iId)} ,
          data: `extra data` ,
          want: `002f{"info":"op does not support data","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "NodeId":"none"} ,
          want: `0023{"info":"login failed","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId+111111), "NodeId":fmt.Sprint(iId+111111)} ,
          want: `002d{"info":"node already connected","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "NodeId":fmt.Sprint(iId)} ,
          want: `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+111111), Type:eForUser} }} ,
          data: `data for Id:zyx` ,
          want: `0023{"id":"zyx","op":"ack","type":"ok"}`+"\n"+
                `003a{"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"delivery"}data for Id:zyx` ,
      },{ head: tMsg{"Op":ePing, "Id":"123", "From":"test1", "To":"test2"} ,
          want: `0023{"id":"123","op":"ack","type":"ok"}`+"\n"+
                `0036{"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"ping"}ping from test1` ,
      }}
   }
   return aTc
}

func (o *tTestClient) verifyRead(iBuf []byte) (int, error) {
   var aMsg []byte
   o.count++

   if o.action == eActVerifyRecv {
      if o.count == 1 {
         aMsg = PackMsg(tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "NodeId":fmt.Sprint(o.id)}, nil)
      } else {
         select {
         case <-sTestVerifyDone:
            return 0, &net.OpError{Op:"read", Err:tError("log out")}
         case aId := <-o.ack:
            aMsg = PackMsg(tMsg{"Op":eAck, "Id":aId, "Type":"n"}, nil)
         }
      }
   } else {
      time.Sleep(20 * time.Millisecond)
      if sTestVerifyGot[0] + sTestVerifyGot[1] != sTestVerifyWant {
         fmt.Printf("Verify FAIL:\n  want: %s   got: %s%s", sTestVerifyWant,
                    sTestVerifyGot[0], sTestVerifyGot[1])
      }
      if o.count-1 == len(o.work) {
         close(sTestVerifyDone)
         return 0, &net.OpError{Op:"read", Err:tError("log out")}
      }
      aWk := o.work[o.count-1]
      sTestVerifyWant = aWk.want + "\n"
      sTestVerifyGot[0] = ""
      sTestVerifyGot[1] = ""
      aMsg = aWk.msg
      if aMsg == nil { aMsg = PackMsg(aWk.head, []byte(aWk.data)) }
   }
   return copy(iBuf, aMsg), nil
}

func (o *tTestClient) cycleRead(iBuf []byte) (int, error) {
   if o.count % 20 == 19 {
      return 0, &net.OpError{Op:"read", Err:tError("log out")}
   }

   var aDlC <-chan time.Time
   if !o.readDeadline.IsZero() {
      aDl := time.NewTimer(o.readDeadline.Sub(time.Now()))
      defer aDl.Stop()
      aDlC = aDl.C
   }

   aTmr := time.NewTimer(200 * time.Millisecond)
   defer aTmr.Stop()

   var aHead tMsg
   var aData string

   select {
   case aId := <-o.ack:
      aHead = tMsg{"Op":eAck, "Id":aId, "Type":"n"}
   case <-aTmr.C:
      o.count++
      if o.count == 1 {
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
   fmt.Printf("%d PUT %s\n", o.id, string(aMsg))
   return copy(iBuf, aMsg), nil
}

func (o *tTestClient) Read(iBuf []byte) (int, error) {
   switch o.action {
   case eActVerifySend, eActVerifyRecv:
      return o.verifyRead(iBuf)
   case eActDeferLogin:
      time.Sleep(kTestLoginWait)
   }
   return o.cycleRead(iBuf)
}

func (o *tTestClient) Write(iBuf []byte) (int, error) {
   if o.closed {
      fmt.Printf("%d testclient.write was closed\n", o.id)
      return 0, &net.OpError{Op:"write", Err:tError("closed")}
   }

   aHeadLen,_ := strconv.ParseInt(string(iBuf[:4]), 16, 0)
   var aHead tMsg
   err := json.Unmarshal(iBuf[4:aHeadLen+4], &aHead)
   if err != nil { panic(err) }

   if o.action >= eActVerifySend {
      if !(o.action == eActVerifyRecv && o.count == 1) {
         if aHead["from"] != nil {
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#id#`, aHead["id"].(string), 1)
         }
         aI := 0; if o.action == eActVerifyRecv { aI = 1 }
         sTestVerifyGot[aI] = string(iBuf) + "\n"
      }
   } else {
      fmt.Printf("%d got %s\n", o.id, string(iBuf))
   }

   if aHead["from"] != nil {
      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.ack <- aHead["id"].(string):
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Printf("%d testclient.write timed out on ack\n", o.id)
         return 0, &net.OpError{Op:"write", Err:tError("noack")}
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
   var fResume func()
   if o.action >= eActVerifySend {
      select {
      case <-sTestVerifyDone:
         return nil
      default:
         aTc := newTestClient(eActVerifySend, o.id)
         aTc.count = o.count
         fResume = func(){ NewLink(aTc) }
      }
   } else {
      fResume = func(){ sTestClientId <- o.id }
   }
   time.AfterFunc(10*time.Millisecond, fResume)
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

