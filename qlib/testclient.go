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
var sTestVerifyGot [3]string // actual results of verifyRead()
var sTestVerifyFail int
var sTestNodeIds map[int]string = make(map[int]string)

func LocalTest(i int) {
   sTestVerifyDone = make(chan int)
   sTestVerifyWant = "\n"
   sTestVerifyGot[2] = "\n"

   UDb.TempUser("u100002", testMakeNode(100002))
   UDb.TempUser("u100003", testMakeNode(100003))
   UDb.TempAlias("u100002", "test1")
   UDb.TempAlias("u100002", "test11")
   UDb.TempAlias("u100003", "test2")
   UDb.TempGroup("blab", "u100002", "test1") // Status eStatInvited

   NewLink(newTestClient(eActVerifyRecv, 100003))
   NewLink(newTestClient(eActVerifySend, 100002))
   <-sTestVerifyDone
   time.Sleep(10 * time.Millisecond)
   fmt.Printf("%d verify pass failures, starting cycle\n\n", sTestVerifyFail)

   sTestClientId = make(chan int, i)
   for a := 0; a <= i; a++ {
      aId := 111000 + a
      aS := fmt.Sprint(aId)
      UDb.TempUser("u"+aS, testMakeNode(aId))
      UDb.TempAlias("u"+aS, "a"+aS)
      UDb.TempGroup("g"+fmt.Sprint(a/100), "u"+aS, "a"+aS)
      if a < i {
         sTestClientId <- aId
      }
   }
   for a := 0; true; a++ {
      NewLink(newTestClient(eActCycle, <-sTestClientId))
   }
}

func testMakeNode(id int) string {
   aNodeId := sBase32.EncodeToString([]byte(fmt.Sprint(id)))
   sTestNodeIds[id] = aNodeId
   aNodeSha, err := getNodeSha(&aNodeId)
   if err != nil { panic(err) }
   return aNodeSha
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
const ( eActCycle tTestAction =iota; eActVerifySend; eActVerifyRecv )

type tTestWork struct { msg []byte; head tMsg; data, want string }

func newTestClient(iAct tTestAction, iId int) *tTestClient {
   aTc := &tTestClient{
      id: iId, to: iId+1,
      action: iAct,
      ack: make(chan string, 10),
   }
   if iAct == eActVerifySend {
      aTmtpRev := tTestWork{
         head: tMsg{"Op":eTmtpRev, "Id":"1"} ,
         want: `0019{"id":"1","op":"tmtprev"}` ,
      }
      aTc.work = []tTestWork{
        { msg : []byte(`00z1{"Op":3, "Uid":"noone"}`) ,
          want: `002c{"info":"invalid header length","op":"quit"}` ,
      },{ msg : []byte(`000a{"Op":12f3`) ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "NoId":"none"} ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eTmtpRev, "Id":"1"} ,
          want: `002f{"info":"disallowed op repetition","op":"quit"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"noone", "Node":"none"} ,
          want: `002a{"info":"tmtprev was omitted","op":"quit"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":1, "For":[]tHeaderFor{{}}} ,
          data: `1` ,
          want: `003c{"info":"disallowed op on unauthenticated link","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"_"} ,
          want: `0070{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"LongJohn Silver"} ,
          want: `0070{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eRegister, "NewNode":"blue", "NewAlias":"short"} ,
          want: `0099{"error":"newalias must be 8+ characters",`+
                     `"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `0036{"info":"disallowed op on connected link","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId], "Datalen":5} ,
          data: `extra` ,
          want: `0025{"info":"invalid header","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eLogin, "Uid":"noone", "Node":"none"} ,
          want: `002b{"info":"corrupt base32 value","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eLogin, "Uid":"noone", "Node":"LB27ML46"} ,
          want: `0023{"info":"login failed","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId+1), "Node":sTestNodeIds[iId+1]} ,
          want: `002d{"info":"node already connected","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `001f{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":15, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+1), Type:eForUser} }} ,
          data: `data for Id:zyx` ,
          want: `0032{"id":"zyx","msgid":"#mid#","op":"ack"}`+"\n"+
                `0047{"datalen":15,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"delivery"}data for Id:zyx` ,
      },{ head: tMsg{"Op":ePing, "Id":"123", "Datalen":1, "To":"test2"} ,
          data: `1` ,
          want: `0032{"id":"123","msgid":"#mid#","op":"ack"}`+"\n"+
                `004f{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"ping","to":"test2"}1` ,
      },{ head: tMsg{"Op":eUserEdit, "Id":"0", "Newalias":"sam walker"} ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `005a{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","id":"#sid#","newalias":"sam walker","op":"user"}` ,
      },{ head: tMsg{"Op":eUserEdit, "Id":"0", "Newnode":"ref"} ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `0076{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","id":"#sid#","nodeid":"#nid#","op":"user"}` ,
      },{ head: tMsg{"Op":eGroupEdit, "Id":"0", "Gid":"blab", "Act":"join"} ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `006e{"act":"join","alias":"test1","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"blab","id":"#sid#","op":"member"}` ,
      },{ head: tMsg{"Op":eGroupEdit, "Id":"0", "Gid":"blab", "Act":"drop", "To":"test1"} ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `006e{"act":"drop","alias":"test1","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"blab","id":"#sid#","op":"member"}` ,
      },{ head: tMsg{"Op":eGroupInvite, "Id":"0", "Gid":"talk", "Datalen":5, "From":"test1", "To":"test2"} ,
          data: `hello` ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `0070{"act":"invite","alias":"test2","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","id":"#sid#","op":"member"}`+"\n"+
                `005e{"datalen":5,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","id":"#id#","op":"invite","to":"test2"}hello` ,
      },{ head: tMsg{"Op":eGroupEdit, "Id":"0", "Gid":"talk", "Act":"alias", "Newalias":"test11"} ,
          want: `0030{"id":"0","msgid":"#mid#","op":"ack"}`+"\n"+
                `0083{"act":"alias","alias":"test1","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","id":"#sid#","newalias":"test11","op":"member"}` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },  aTmtpRev,
        { msg : []byte(`0034{"Op":2, "Uid":"u`+fmt.Sprint(iId)+`", "Node":"`+sTestNodeIds[iId]+`"}`+
                       `002f{"Op":7, "Id":"123", "Datalen":1, "To":"test2"}1`) ,
          want: `001f{"info":"login ok","op":"info"}`+"\n"+
                `0032{"id":"123","msgid":"#mid#","op":"ack"}`+"\n"+
                `004f{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"ping","to":"test2"}1` ,
      },{ head: tMsg{"Op":ePost, "Id":"zyx", "Datalen":15, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+1), Type:eForUser} }} ,
          data: `data for Id` ,
      },{ msg : []byte(`:zyx`) ,
          want: `0032{"id":"zyx","msgid":"#mid#","op":"ack"}`+"\n"+
                `0047{"datalen":15,"from":"u`+fmt.Sprint(iId)+`","id":"#id#","op":"delivery"}data for Id:zyx` ,
      },{ head: tMsg{"Op":eQuit} ,
          want: `0020{"info":"logout ok","op":"quit"}` ,
      },{ msg : []byte(`delay`) ,
          want: `0024{"info":"login timeout","op":"quit"}` ,
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
         aMsg = PackMsg(tMsg{"Op":eTmtpRev, "Id":"1"}, aMsg)
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
      aGot := strings.Join(sTestVerifyGot[:], "")
      if aGot != sTestVerifyWant {
         sTestVerifyFail++
         fmt.Printf("Verify FAIL:\n  want: %s   got: %s", sTestVerifyWant, aGot)
      }
      if o.count-1 == len(o.work) {
         close(sTestVerifyDone)
         return 0, &net.OpError{Op:"read", Err:tError("log out")}
      }
      aWk := o.work[o.count-1]
      aNl := "\n"; if aWk.want == "" { aNl = "" }
      sTestVerifyWant = aWk.want + aNl
      sTestVerifyGot[0], sTestVerifyGot[1], sTestVerifyGot[2] = "","",""
      aMsg = aWk.msg
      if aMsg == nil {
         aMsg = PackMsg(aWk.head, []byte(aWk.data))
      } else if string(aMsg[:5]) == "delay" {
         return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
      }
      select {
      case aId := <-o.ack:
         aMsg = PackMsg(tMsg{"Op":eAck, "Id":aId, "Type":"n"}, aMsg)
      default:
      }
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
         aHead = tMsg{"Op":eTmtpRev, "Id":"1"}
      } else if o.count == 2 {
         aHead = tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id]}
      } else if o.id == 111001 && o.count % 20 == 3 {
         aHead = tMsg{"Op":ePing, "Id":fmt.Sprint(o.count), "Datalen":1, "From":"a111001", "To":"a111000"}
         aData = "1"
      } else {
         aFor := tHeaderFor{Id:"u"+fmt.Sprint(o.to), Type:eForUser}
         if o.count % 20 >= 18 { aFor = tHeaderFor{Id:"g0", Type:eForGroupAll} }
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
   if o.action == eActCycle {
      return o.cycleRead(iBuf)
   }
   return o.verifyRead(iBuf)
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
         aI := 0; if o.action == eActVerifyRecv { aI = 2 }
         if aHead["msgid"] != nil {
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#mid#`, aHead["msgid"].(string), 1)
         } else if aHead["from"] != nil {
            aRepl := `#id#`; if o.action == eActVerifySend { aRepl = `#sid#`; aI = 1 }
            sTestVerifyWant = strings.Replace(sTestVerifyWant, aRepl, aHead["id"].(string), 1)
         } else if aHead["op"].(string) == "registered" {
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#uid#`, aHead["uid"].(string), 1)
         }
         if aHead["nodeid"] != nil {
            sTestVerifyWant = strings.Replace(sTestVerifyWant, `#nid#`, aHead["nodeid"].(string), 1)
         }
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

