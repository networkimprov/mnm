// Copyright 2017, 2018 Liam Breck
// Published at https://github.com/networkimprov/mnm
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/

package qlib

import (
   "sync/atomic"
   "hash/crc32"
   "fmt"
   "io"
   "encoding/json"
   "net"
   "os"
   "os/signal"
   "strconv"
   "strings"
   "sync"
   "time"
)


const kTestLoginWait time.Duration = 6 * time.Second

var sTestNodeIds = make(map[int]string)
var sTestVerifyDone = make(chan int)
var sTestVerifyWant struct { val string; sync.Mutex } // expected results of _verifyRead()
var sTestVerifyGot [3]string // actual results of _verifyRead()
var sTestVerifyFail int
var sTestClientCount int32
var sTestClientId chan int
var sTestLogins = make(map[int]*int)
var sTestLoginTotal int32
var sTestRecvCount, sTestRecvOhi int32
var sTestRecvBytes int64
var sTestReadSize = [...]int{50, 50, 50, 500, 500, 1500, 2000, 5000, 10000, 50000}
var sTestReadData = make([]byte, 16*1024)

func LocalTest(i int) {
   sTestVerifyWant.val = "\n"
   sTestVerifyGot[2] = "\n"

   UDb.TempUser("u100002", _testMakeNode(100002))
   UDb.TempUser("u100003", _testMakeNode(100003))
   UDb.TempAlias("u100002", "test1")
   UDb.TempAlias("u100002", "test11")
   UDb.TempAlias("u100003", "test2")
   UDb.TempGroup("blab", "u100002", "test1") // Status eStatInvited

   NewLink(_newTestClient(eActVerifyRecv, 100003))
   NewLink(_newTestClient(eActVerifySend, 100002))
   <-sTestVerifyDone
   time.Sleep(10 * time.Millisecond)
   UDb.Erase() // assumes no userdb write ops during cycle
   fmt.Fprintf(os.Stderr, "%d verify pass failures, starting cycle\n\n", sTestVerifyFail)

   aSegment := []byte(`0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz.!`)
   for a := 0; a < len(sTestReadData) / len(aSegment); a++ {
      copy(sTestReadData[a*len(aSegment):], aSegment)
   }

   sTestClientCount = int32(i)
   sTestClientId = make(chan int, i)
   for a := 0; a <= i; a++ {
      aId := 111000 + a
      aS := fmt.Sprint(aId)
      UDb.TempUser("u"+aS, _testMakeNode(aId))
      UDb.TempAlias("u"+aS, "a"+aS)
      UDb.TempGroup("g"+fmt.Sprint(a/100), "u"+aS, "a"+aS)
      if a < i {
         sTestLogins[aId] = new(int)
         sTestClientId <- aId
      }
   }

   aIntWatch := make(chan os.Signal, 1)
   signal.Notify(aIntWatch, os.Interrupt)
   for aLoop := true; aLoop; {
      select {
      case <-aIntWatch:
         aLoop = false
      case aId := <-sTestClientId:
         NewLink(_newTestClient(eActCycle, aId))
      }
   }
   fmt.Fprintf(os.Stderr, " shutting down\n")
   Suspend()
   _ = os.RemoveAll(sStore.Root)
}

func _testMakeNode(id int) string {
   aNodeId := sBase32.EncodeToString([]byte(fmt.Sprint(id)))
   sTestNodeIds[id] = aNodeId
   aNodeSha, err := getNodeSha(&aNodeId)
   if err != nil { panic(err) }
   return aNodeSha
}

type tTestClient struct {
   id int // user id
   count int // msg number
   toRead, toWrite int // data yet to read/write
   action tTestAction // test mode
   work []tTestWork // verify sequence data
   ack chan string // writer tells reader to issue ack to qlib
   closed bool // when about to shut down
   readDeadline time.Time // set by qlib
}

type tTestAction int
const ( eActCycle tTestAction =iota; eActVerifySend; eActVerifyRecv )

type tTestWork struct { msg []byte; head tMsg; data, want string }

type tTestForOhi struct { Id string }

func _newTestClient(iAct tTestAction, iId int) *tTestClient {
   aTc := &tTestClient{
      id: iId,
      action: iAct,
      ack: make(chan string, 10),
   }
   if iAct == eActVerifySend {
      aTmtpRev := tTestWork{
         head: tMsg{"Op":eOpTmtpRev, "Id":"1"} ,
         want: `{"id":"1","op":"tmtprev"}` ,
      }
      aTc.work = []tTestWork{
        { msg : []byte(`00z1{"Op":3, "Uid":"noone"}`) ,
          want: `{"error":"invalid header length","op":"quit"}` ,
      },{ msg : []byte(`000a{"Op":12f3`) ,
          want: `{"error":"invalid header","op":"quit"}` ,
      },{ head: tMsg{"Op":eOpLogin, "Uid":"noone", "NoId":"none"} ,
          want: `{"error":"invalid header","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpTmtpRev, "Id":"1"} ,
          want: `{"error":"disallowed op repetition","op":"quit"}` ,
      },{ head: tMsg{"Op":eOpLogin, "Uid":"noone", "Node":"none"} ,
          want: `{"error":"tmtprev was omitted","op":"quit"}` ,
      },{ head: tMsg{"Op":eOpPost, "Id":"zyx", "Datalen":1, "For":[]tHeaderFor{{Id:"x", Type:eForUser}}} ,
          data: `1` ,
          want: `{"error":"disallowed op on unauthenticated link","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpRegister, "NewNode":"blue", "NewAlias":"_"} ,
          want: `{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eOpQuit} ,
          want: `{"error":"logout ok","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpRegister, "NewNode":"blue", "NewAlias":"LongJohn Silver"} ,
          want: `{"nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eOpQuit} ,
          want: `{"error":"logout ok","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpRegister, "NewNode":"blue", "NewAlias":"short"} ,
          want: `{"error":"newalias must be 8+ characters","nodeid":"#nid#","op":"registered","uid":"#uid#"}`+"\n"+
                `{"info":"login ok","op":"info"}` ,
      },{ head: tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `{"error":"disallowed op on connected link","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId], "Datalen":5} ,
          data: `extra` ,
          want: `{"error":"invalid header","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"noone", "Node":"none"} ,
          want: `{"error":"corrupt base32 value","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"noone", "Node":"LB27ML46"} ,
          want: `{"error":"login failed","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(iId+1), "Node":sTestNodeIds[iId+1]} ,
          want: `{"error":"node already connected","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `{"info":"login ok","op":"info"}`+"\n"+
                `{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","node":"tbd","op":"login","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpPost, "Id":"zyx", "Datalen":15, "Datahead":5, "Datasum":1, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+1), Type:eForUser} }} ,
          data: `data for Id:zyx` ,
          want: `{"id":"zyx","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datahead":5,"datalen":15,"datasum":1,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"delivery","posted":"#pdt#"}data for Id:zyx` ,
      },{ head: tMsg{"Op":eOpPostNotify, "Id":"id", "Datalen":14, "Datahead":5, "Datasum":5,
                     "For":[]tHeaderFor{{Id:"u"+fmt.Sprint(iId+1), Type:eForUser}}, "Fornotself":true,
                     "Notelen":5, "Notehead":1, "Notesum":1} , //todo add Notefor
          data: `note.post data` ,
          want: `{"id":"id","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datahead":5,"datalen":9,"datasum":5,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"delivery","posted":"#pdt#"}post data`+"\n"+
                `{"datahead":1,"datalen":5,"datasum":1,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"notify","posted":"#pdt#","postid":"#pid#"}note.` ,
      },{ head: tMsg{"Op":eOpPing, "Id":"123", "Datalen":1, "To":"test2"} ,
          data: `1` ,
          want: `{"id":"123","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"ping","posted":"#pdt#","to":"test2"}1` ,
      },{ head: tMsg{"Op":eOpUserEdit, "Id":"0", "Newalias":"sam walker"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","newalias":"sam walker","op":"user","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpUserEdit, "Id":"0", "Newnode":"ref"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","nodeid":"#nid#","op":"user","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpGroupEdit, "Id":"0", "Gid":"blab", "Act":"join"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"act":"join","alias":"test1","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"blab","headsum":#sck#,"id":"#sid#","op":"member","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpGroupEdit, "Id":"0", "Gid":"blab", "Act":"drop", "To":"test1"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}` ,
      },{ head: tMsg{"Op":eOpGroupInvite, "Id":"0", "Gid":"talk", "Datalen":5, "From":"test1", "To":"test2"} ,
          data: `hello` ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"act":"invite","alias":"test2","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","headsum":#sck#,"id":"#sid#","op":"member","posted":"#spdt#"}`+"\n"+
                `{"datalen":5,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","headsum":#ck#,"id":"#id#","op":"invite","posted":"#pdt#","to":"test2"}hello` ,
      },{ head: tMsg{"Op":eOpGroupEdit, "Id":"0", "Gid":"talk", "Act":"alias", "Newalias":"test11"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"act":"alias","alias":"test1","datalen":0,"from":"u`+fmt.Sprint(iId)+`","gid":"talk","headsum":#sck#,"id":"#sid#","newalias":"test11","op":"member","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpPing, "Id":"123", "Datalen":144, "To":"test2"} ,
          data: `123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 123456789 1234` ,
          want: `{"error":"data too long for request type","op":"quit"}` ,
      },  aTmtpRev,
        { head: tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(iId), "Node":sTestNodeIds[iId]} ,
          want: `{"info":"login ok","op":"info"}`+"\n"+
                `{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","node":"tbd","op":"login","posted":"#spdt#"}` ,
      },{ head: tMsg{"Op":eOpOhiEdit, "Id":"0", "For":[]tTestForOhi{{Id:"u"+fmt.Sprint(iId+1)}}, "Type":"add"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"from":"u`+fmt.Sprint(iId)+`","op":"ohi","status":1}` ,
      },{ head: tMsg{"Op":eOpOhiEdit, "Id":"0", "For":[]tTestForOhi{{Id:"u"+fmt.Sprint(iId+1)}}, "Type":"drop"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":0,"for":[{"Id":"u`+fmt.Sprint(iId+1)+`","Type":0}],"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","op":"ohiedit","posted":"#spdt#","type":"drop"}`+"\n"+
                `{"from":"u`+fmt.Sprint(iId)+`","op":"ohi","status":2}` ,
      },{ head: tMsg{"Op":eOpOhiEdit, "Id":"0", "For":[]tTestForOhi{{Id:"u"+fmt.Sprint(iId+1)}}, "Type":"add"} ,
          want: `{"id":"0","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":0,"for":[{"Id":"u`+fmt.Sprint(iId+1)+`","Type":0}],"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","op":"ohiedit","posted":"#spdt#","type":"add"}`+"\n"+
                `{"from":"u`+fmt.Sprint(iId)+`","op":"ohi","status":1}` ,
      },{ head: tMsg{"Op":eOpPulse} ,
      },{ head: tMsg{"Op":eOpQuit} ,
          want: `{"error":"logout ok","op":"quit"}`+"\n"+
                `{"from":"u`+fmt.Sprint(iId)+`","op":"ohi","status":2}` ,
      },  aTmtpRev,
        { msg : []byte(`0034{"Op":2, "Uid":"u`+fmt.Sprint(iId)+`", "Node":"`+sTestNodeIds[iId]+`"}`+
                       `002f{"Op":9, "Id":"123", "Datalen":1, "To":"test2"}1`) ,
          want: `{"info":"login ok","op":"info"}`+"\n"+
                `{"id":"123","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":0,"from":"u`+fmt.Sprint(iId)+`","headsum":#sck#,"id":"#sid#","node":"tbd","op":"login","posted":"#spdt#"}`+"\n"+
                `{"datalen":1,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"ping","posted":"#pdt#","to":"test2"}1` ,
      },{ head: tMsg{"Op":eOpPost, "Id":"zyx", "Datalen":15, "For":[]tHeaderFor{
                       {Id:"u"+fmt.Sprint(iId+1), Type:eForUser} }} ,
          data: `data for Id` ,
      },{ msg : []byte(`:zyx`) ,
          want: `{"id":"zyx","msgid":"#mid#","op":"ack","posted":"#pst#"}`+"\n"+
                `{"datalen":15,"from":"u`+fmt.Sprint(iId)+`","headsum":#ck#,"id":"#id#","op":"delivery","posted":"#pdt#"}data for Id:zyx` ,
      },{ head: tMsg{"Op":eOpPing, "Id":"123", "Datalen":8, "To":"test2"} ,
      },{ msg : []byte(`1234567`) ,
      },{ msg : []byte{255,254,253} ,
          want: `{"error":"data not valid UTF8","op":"quit"}` ,
      },{ msg : []byte(`delay`) ,
          want: `{"error":"connection timeout","op":"quit"}` ,
      }}
   }
   return aTc
}

func (o *tTestClient) _verifyRead(iBuf []byte) (int, error) {
   var aMsg []byte
   o.count++

   if o.action == eActVerifyRecv {
      if o.count == 1 {
         aMsg = packMsg(tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id]}, nil)
         aMsg = packMsg(tMsg{"Op":eOpTmtpRev, "Id":"1"}, aMsg)
      } else {
         select {
         case <-sTestVerifyDone:
            return 0, io.EOF
         case aId := <-o.ack:
            aMsg = packMsg(tMsg{"Op":eOpAck, "Id":aId, "Type":"n"}, nil)
         }
      }
   } else {
      time.Sleep(20 * time.Millisecond)
      aGot := strings.Join(sTestVerifyGot[:], "")
      if aGot != sTestVerifyWant.val {
         sTestVerifyFail++
         fmt.Fprintf(os.Stderr, "Verify FAIL:\n  want: %s   got: %s", sTestVerifyWant.val, aGot)
      }
      if o.count-1 == len(o.work) {
         close(sTestVerifyDone)
         return 0, io.EOF
      }
      aWk := o.work[o.count-1]
      aNl := "\n"; if aWk.want == "" { aNl = "" }
      sTestVerifyWant.val = aWk.want + aNl
      sTestVerifyGot[0], sTestVerifyGot[1], sTestVerifyGot[2] = "","",""
      aMsg = aWk.msg
      if aMsg == nil {
         aMsg = packMsg(aWk.head, []byte(aWk.data))
      } else if string(aMsg) == "delay" {
         return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
      }
      select {
      case aId := <-o.ack:
         aMsg = packMsg(tMsg{"Op":eOpAck, "Id":aId, "Type":"n"}, aMsg)
      default:
      }
   }
   return copy(iBuf, aMsg), nil
}

func _testLoginSummary() {
   if atomic.AddInt32(&sTestLoginTotal, 1) % (sTestClientCount * 1) == 0 {
      var aMinV, aMaxV, aMinK, aMaxK int = 1e9, 0,0,0
      for aK, aV := range sTestLogins {
         if *aV > aMaxV { aMaxV = *aV; aMaxK = aK }
         if *aV < aMinV { aMinV = *aV; aMinK = aK }
      }
      fmt.Fprintf(os.Stderr, "login summary: min u%d %d, max u%d %d\n", aMinK, aMinV, aMaxK, aMaxV)
   }
}

func _testRecvSummary(i int) {
   aB := atomic.AddInt64(&sTestRecvBytes, int64(i))
   aN := atomic.AddInt32(&sTestRecvCount, 1)
   if aN % (sTestClientCount * 2) == 0 {
      fmt.Fprintf(os.Stderr, "messages %d, ohis %d, MB %d\n", aN, sTestRecvOhi, aB/(1024*1024))
   }
}

func (o *tTestClient) _cycleRead(iBuf []byte) (int, error) {
   fGetBuf := func() []byte {
      cData := sTestReadData
      if o.toRead < len(cData) {
         cData = cData[:o.toRead]
      }
      o.toRead -= len(cData)
      return cData
   }

   if o.toRead > 0 {
      time.Sleep(3 * time.Millisecond)
      return copy(iBuf, fGetBuf()), nil
   }

   var aDlC <-chan time.Time
   if !o.readDeadline.IsZero() {
      aDl := time.NewTimer(o.readDeadline.Sub(time.Now()))
      defer aDl.Stop()
      aDlC = aDl.C
   }

   aNs := time.Now().Nanosecond()
   if o.count < 2 || o.count == 19 { aNs = 30 }
   aTmr := time.NewTimer(time.Duration(aNs % 30 + 1) * time.Second)
   defer aTmr.Stop()

   var aHead tMsg
   var aData []byte

   select {
   case aId := <-o.ack:
      time.Sleep(time.Duration(aNs % 100 + 1) * time.Millisecond)
      aHead = tMsg{"Op":eOpAck, "Id":aId, "Type":"n"}
   case <-aTmr.C:
      o.count++
      if o.count == 1 {
         aHead = tMsg{"Op":eOpTmtpRev, "Id":"1"}
      } else if o.count == 2 {
         aHead = tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id]}
         *sTestLogins[o.id]++
         _testLoginSummary()
      } else if o.count == 3 {
         var aFor []tTestForOhi
         aMax := int(sTestClientCount); if aMax > 100 { aMax = 100 }
         for a := 0; a < aMax; a++ {
            aFor = append(aFor, tTestForOhi{Id:"u"+fmt.Sprint(o.id/aMax*aMax+a)})
         }
         aHead = tMsg{"Op":eOpOhiEdit, "Id":fmt.Sprint(o.count), "For":aFor, "Type":"add"}
      } else if o.count == 4 && o.id % 2 == 1 {
         aData = []byte("bing-bong!")
         aHead = tMsg{"Op":eOpPing, "Id":fmt.Sprint(o.count), "Datalen":len(aData),
                      "From":"a"+fmt.Sprint(o.id), "To":"a"+fmt.Sprint(o.id-1)}
      } else if o.count < 20 {
         var aFor []tHeaderFor
         if o.count < 18 {
            aTo := time.Now().Nanosecond() % int(sTestClientCount) + 111000
            aFor = []tHeaderFor{{Id:"u"+fmt.Sprint(aTo)  , Type:eForUser},
                                {Id:"u"+fmt.Sprint(aTo+1), Type:eForUser}}
         } else {
            aFor = []tHeaderFor{{Id:"g"+fmt.Sprint((o.id-111000)/100), Type:eForGroupAll}}
            if o.count == 19 { aFor[0].Type = eForGroupExcl }
         }
         o.toRead = sTestReadSize[time.Now().Nanosecond() % len(sTestReadSize)]
         aHead = tMsg{"Op":eOpPost, "Id":fmt.Sprint(o.count), "Datalen":o.toRead, "For":aFor}
         aData = fGetBuf()
      } else {
         return 0, io.EOF
      }
   case <-aDlC:
      return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
   }

   aMsg := packMsg(aHead, aData)
   //fmt.Printf("%d PUT %s\n", o.id, string(aMsg))
   return copy(iBuf, aMsg), nil
}

func (o *tTestClient) Read(iBuf []byte) (int, error) {
   if o.action == eActCycle {
      return o._cycleRead(iBuf)
   }
   return o._verifyRead(iBuf)
}

func _testVerifyWantEdit(iOld, iNew string) {
   sTestVerifyWant.Lock()
   sTestVerifyWant.val = strings.Replace(sTestVerifyWant.val, "#"+iOld+"#", iNew, 1)
   sTestVerifyWant.Unlock()
}

func (o *tTestClient) Write(iBuf []byte) (int, error) {
   if o.closed {
      return 0, &net.OpError{Op:"write", Err:tError("closed")}
   }

   if o.toWrite > 0 {
      o.toWrite -= len(iBuf)
      return len(iBuf), nil
   }

   aHeadLen,_ := strconv.ParseInt(string(iBuf[:4]), 16, 0)
   var aHead tMsg
   err := json.Unmarshal(iBuf[4:aHeadLen+4], &aHead)
   if err != nil { panic(err) }

   aOp := aHead["op"].(string)
   if o.action >= eActVerifySend {
      if o.action == eActVerifySend || !(aOp == "tmtprev" || aOp == "info" || aOp == "login") {
         aI := 0; if o.action == eActVerifyRecv { aI = 2 }
         if aHead["msgid"] != nil {
            _testVerifyWantEdit("mid", aHead["msgid"].(string))
            _testVerifyWantEdit("pst", aHead["posted"].(string))
         } else if aHead["from"] != nil && aOp != "ohi" {
            aS := ""; if o.action == eActVerifySend { aS = "s"; aI = 1 }
            _testVerifyWantEdit(aS+"id", aHead["id"].(string))
            _testVerifyWantEdit(aS+"pdt", aHead["posted"].(string))
            _testVerifyWantEdit(aS+"ck", fmt.Sprint(uint32(aHead["headsum"].(float64))))
         } else if aOp == "registered" {
            _testVerifyWantEdit("uid", aHead["uid"].(string))
         }
         if aHead["nodeid"] != nil {
            _testVerifyWantEdit("nid", aHead["nodeid"].(string))
         }
         if aOp == "notify" {
            _testVerifyWantEdit("pid", aHead["postid"].(string))
         }
         sTestVerifyGot[aI] += string(iBuf[4:]) + "\n"
      }
   } else {
      if aHead["error"] != nil {
         fmt.Fprintf(os.Stderr, "%d testclient.write op %s error %s\n",
                     o.id, aOp, aHead["error"].(string))
      }
      //fmt.Printf("%d got %s\n", o.id, string(iBuf))
   }

   if aOp == "ohi" {
      atomic.AddInt32(&sTestRecvOhi, 1)
   } else if aHead["from"] != nil {
      aHeadsum := uint32(aHead["headsum"].(float64))
      delete(aHead, "headsum")
      if aHeadsum != crc32.Checksum(packMsg(aHead, nil), sCrc32c) {
         fmt.Fprintf(os.Stderr, "%d testclient.write headsum failed\n", o.id)
      }

      aDatalen := int(aHead["datalen"].(float64))
      if o.action == eActCycle { _testRecvSummary(aDatalen) }
      o.toWrite = aDatalen - len(iBuf) + int(aHeadLen+4)

      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.ack <- aHead["id"].(string):
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Fprintf(os.Stderr, "%d testclient.write timed out on ack\n", o.id)
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
   if o.action >= eActVerifySend {
      select {
      case <-sTestVerifyDone:
         return nil
      default:
         aTc := _newTestClient(eActVerifySend, o.id)
         aTc.count = o.count
         time.AfterFunc(10*time.Millisecond, func(){ NewLink(aTc) })
      }
   } else {
      aSec := time.Now().Nanosecond() % 30 + 1
      time.AfterFunc(time.Duration(aSec) * time.Second, func(){ sTestClientId <- o.id })
   }
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

