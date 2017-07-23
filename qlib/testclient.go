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
var sTestVerifyFail int
var sTestNodeIds map[int]string = make(map[int]string)

func LocalTest(i int) {
   sTestVerifyDone = make(chan int)
   sTestVerifyWant = "\n"
   sTestVerifyGot[1] = "\n"

   UDb.TempUser(testMakeUser(111112))
   UDb.TempUser(testMakeUser(222223))
   UDb.TempAlias("u111112", "test1")
   UDb.TempAlias("u222223", "test2")

   NewLink(newTestClient(eActVerifyRecv, 222223))
   NewLink(newTestClient(eActVerifySend, 111112))
   <-sTestVerifyDone
   time.Sleep(10 * time.Millisecond)
   fmt.Printf("%d verify pass failures, starting cycle\n\n", sTestVerifyFail)

   UDb.TempUser(testMakeUser(111111))
   UDb.TempUser(testMakeUser(222222))
   UDb.TempUser(testMakeUser(333333))
   UDb.TempAlias("u111111", "a1")
   UDb.TempAlias("u222222", "a2")
   UDb.TempGroup("g1", "u111111", "111")
   UDb.TempGroup("g1", "u222222", "222")
   UDb.TempGroup("g1", "u333333", "333")

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

func testMakeUser(id int) (string, string) {
   aNodeId := sBase32.EncodeToString([]byte(fmt.Sprint(id)))
   sTestNodeIds[id] = aNodeId
   aNodeSha, err := getNodeSha(&aNodeId)
   if err != nil { panic(err) }
   return "u"+fmt.Sprint(id), aNodeSha
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
        { msg : []byte(`00z1{"Op":3, "Uid":"noone"}`) ,
          want: `002c{"info":"invalid header length","op":"quit"}` ,
      },{ msg : []byte(`000a{"Op":12f3`) ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "NoId":"none"} ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":1, "For":[]tHeaderFor{{}}} ,
          data: `1` ,
          want: `003c{"info":"disallowed op on unauthenticated link","op":"quit"}` ,
      },{ head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"_"} ,
          want: `0070{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },{ head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"LongJohn Silver"} ,
          want: `0070{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },{ head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"short"} ,
          want: `0099{"error":"newalias must be 8+ characters",`+
                     `"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `0036{"info":"disallowed op on connected link","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId], "Datalen":5} ,
          data: `extra` ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "Node":"none"} ,
          want: `002b{"info":"corrupt base32 value","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "Node":"LB27ML46"} ,
          want: `0023{"info":"login failed","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId+111111), "Node":sTestNodeIds[iId+111111]} ,
          want: `002d{"info":"node already connected","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":15, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+111111), Type:eForUser} }} ,
          data: `data for Id:zyx` ,
          want: `0023{"id":"zyx","op":"ack","type":"ok"}`+"\n"+
                `0047{"datalen":15,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"delivery"}data for Id:zyx` ,
      },{ head: tMsg{"Op":ePing, "Id":"123", "Datalen":1, "From":"test1", "To":"test2"} ,
          data: `1` ,
          want: `0023{"id":"123","op":"ack","type":"ok"}`+"\n"+
                `0042{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"ping"}1` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },{ msg : []byte(`0034{"Op":3, "Uid":"u`+fmt.Sprint(iId)+`", "Node":"`+sTestNodeIds[iId]+`"}`+
                       `003f{"Op":6, "Id":"123", "Datalen":1, "From":"test1", "To":"test2"}1`) ,
          want: `001f{"info":"login ok","op":"info"}`+"\n"+
                `0023{"id":"123","op":"ack","type":"ok"}`+"\n"+
                `0042{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"ping"}1` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":15, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+111111), Type:eForUser} }} ,
          data: `data for Id` ,
      },{ msg : []byte(`:zyx`) ,
          want: `0023{"id":"zyx","op":"ack","type":"ok"}`+"\n"+
                `0047{"datalen":15,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"delivery"}data for Id:zyx` ,
      }}
   }
   return aTc
}

func (o *tTestClient) verifyRead(iBuf []byte) (int, error) {
   var aMsg []byte
   o.count++

   if o.action == eActVerifyRecv {
      if o.count == 1 {
         aMsg = PackMsg(tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id]}, nil)
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
         sTestVerifyFail++
         fmt.Printf("Verify FAIL:\n  want: %s   got: %s%s", sTestVerifyWant,
                    sTestVerifyGot[0], sTestVerifyGot[1])
      }
      if o.count-1 == len(o.work) {
         close(sTestVerifyDone)
         return 0, &net.OpError{Op:"read", Err:tError("log out")}
      }
      aWk := o.work[o.count-1]
      aNl := "\n"; if aWk.want == "" { aNl = "" }
      sTestVerifyWant = aWk.want + aNl
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
         aHead = tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id]}
      } else if o.id == 222222 && o.count % 20 == 2 {
         aHead = tMsg{"Op":ePing, "Id":fmt.Sprint(o.count), "Datalen":1, "From":"a2", "To":"a1"}
         aData = "1"
      } else {
         aFor := tHeaderFor{Id:"u"+fmt.Sprint(o.to), Type:eForUser}
         if o.count % 20 >= 18 { aFor = tHeaderFor{Id:"g1", Type:eForGroupAll} }
         if o.count % 20 == 19 { aFor.Type = eForGroupExcl }
         aHead = tMsg{"Op":ePost, "Id":fmt.Sprint(o.count), "Datalen":10, "For":[]tHeaderFor{aFor}}
         aData = fmt.Sprintf(" |msg %3d|", o.count)
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
         } else if aHead["op"].(string) == "registered" {
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#nid#`, aHead["nodeid"].(string), 1)
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#uid#`, aHead["uid"].(string), 1)
         }
         aI := 0; if o.action == eActVerifyRecv { aI = 1 }
         sTestVerifyGot[aI] += string(iBuf) + "\n"
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

