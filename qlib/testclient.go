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

var sTestNodeIds = make(map[int][]string)
var sTestVerifyWork []tTestWork
var sTestVerifyDone = make(chan int)
var sTestVerifyOp int
var sTestVerifyNfsn bool
var sTestVerifyWant struct { val string; sync.Mutex } // expected results
var sTestVerifyGot [3]string // actual results: response to sender, msg to sender, msg to receiver
var sTestVerifyGotNode = make(map[int]string) // actual results at nodes
var sTestVerifyFail int
var sTestClientCount int32
var sTestClientId chan [3]int
var sTestLogins = make(map[int]*int)
var sTestLoginTotal int32
var sTestRecvCount, sTestRecvOhi int32
var sTestRecvBytes int64
var sTestReadSize = [...]int{80, 80, 80, 80, 400, 400, 2000, 2000, 10000, 50000, 250000}
var sTestReadData = make([]byte, 16*1024)

func LocalTest(i int) {
   sTestVerifyGotNode[100002] = ""
   sTestVerifyGotNode[100003] = ""
   UDb.TempUser("u100002", _testMakeNode(100002, 0))
   UDb.TempUser("u100003", _testMakeNode(100003, 0))
   UDb.TempNode("u100002", _testMakeNode(100002, 1))
   UDb.TempNode("u100003", _testMakeNode(100003, 1))
   UDb.TempAlias("u100002", "test1")
   UDb.TempAlias("u100002", "test11")
   UDb.TempAlias("u100003", "test2")
   UDb.TempGroup("blab", "u100002", "test1") // Status eStatInvited

   aFd, err := os.Open("test.json")
   if err != nil {
      fmt.Fprintf(os.Stderr, "%s\n", err)
      return
   }
   err = json.NewDecoder(aFd).Decode(&sTestVerifyWork)
   aFd.Close()
   if err != nil {
      fmt.Fprintf(os.Stderr, "test.json %s\n", err)
      return
   }
   for a := range sTestVerifyWork {
      aWk := &sTestVerifyWork[a]
      if aWk.Tmtp > 0 {
         aWk.Head, aWk.wants = sTestVerifyWork[0].Head, sTestVerifyWork[0].wants
         aWk.Msg, aWk.Data, aWk.Datb = "", "", nil
         continue
      }
      fFor := func(cObj interface{}) {
         if cFor, _ := cObj.([]interface{}); cFor != nil {
            for c := range cFor {
               if cEl, _ := cFor[c].(map[string]interface{}); cEl != nil {
                  switch cId, _ := cEl["Id"].(string); cId {
                  case "*senduid": cEl["Id"] = "u100002"
                  case "*recvuid": cEl["Id"] = "u100003"
                  }
               }
            }
         }
      }
      if aWk.Head != nil {
         if aOp, _ := aWk.Head["Op"].(string); aOp != "" {
            aWk.Head["Op"] = kOpSet[aOp]
         }
         switch aUid, _ := aWk.Head["Uid"].(string); aUid {
         case "*senduid": aWk.Head["Uid"] = "u100002"
         case "*recvuid": aWk.Head["Uid"] = "u100003"
         }
         switch aNode, _ := aWk.Head["Node"].(string); aNode {
         case "*sendnode": aWk.Head["Node"] = sTestNodeIds[100002][0]
         case "*recvnode": aWk.Head["Node"] = sTestNodeIds[100003][0]
         }
         fFor(aWk.Head["For"])
      }
      if len(aWk.Want) == 0 { continue }
      aData := make([]string, len(aWk.Want))
      for a1 := range aWk.Want {
         switch aFrom, _ := aWk.Want[a1]["from"].(string); aFrom {
         case "*senduid": aWk.Want[a1]["from"] = "u100002"
         }
         fFor(aWk.Want[a1]["for"])
         if aDataSet, _ := aWk.Want[a1]["~data"].([]interface{}); aDataSet != nil {
            for a2 := range aDataSet {
               aData[a1] += fmt.Sprint(aDataSet[a2])
            }
            aWk.Want[a1]["~data"] = a1
         }
      }
      aBuf, err := json.Marshal(aWk.Want)
      if err != nil {
         fmt.Fprintf(os.Stderr, "test.json %s\n", err)
         return
      }
      aWk.wants = strings.ReplaceAll(string(aBuf[1:len(aBuf)-1]), "},{", "}\n{")
      aWk.wants = strings.ReplaceAll(aWk.wants, `"headsum":1`, `"headsum":#sck#`)
      aWk.wants = strings.ReplaceAll(aWk.wants, `"headsum":2`, `"headsum":#ck#`)
      for a1 := range aData {
         if aData[a1] == "" { continue }
         aWk.wants = strings.Replace(aWk.wants, `,"~data":`+ fmt.Sprint(a1) +`}`, `}`+ aData[a1], 1)
      }
   }

   NewLink(_newTestClient(eActVerifyRecv, [3]int{100003, 0, 0}))
   NewLink(_newTestClient(eActVerifyRecv, [3]int{100003, 1, 0}))
   NewLink(_newTestClient(eActVerifyRecv, [3]int{100002, 1, 0}))
   time.Sleep(10 * time.Millisecond)
   NewLink(_newTestClient(eActVerifySend, [3]int{100002, 0, 0}))
   <-sTestVerifyDone
   time.Sleep(10 * time.Millisecond)
   fmt.Fprintf(os.Stderr, "%d verify pass failures, starting cycle\n\n", sTestVerifyFail)

   sTestClientCount = int32(i)
   sTestClientId = make(chan [3]int, i)
   for a := 0; a <= i; a++ {
      aId := 111000 + a
      aS := fmt.Sprint(aId)
      UDb.TempUser("u"+aS, _testMakeNode(aId, 0))
      UDb.TempAlias("u"+aS, "a"+aS)
      UDb.TempGroup("g"+fmt.Sprint(a/100), "u"+aS, "a"+aS)
      if a < i {
         sTestLogins[aId] = new(int)
         sTestClientId <- [3]int{aId, 0, 0}
      }
   }

   aIntWatch := make(chan os.Signal, 1)
   signal.Notify(aIntWatch, os.Interrupt)
   for aLoop := true; aLoop; {
      select {
      case <-aIntWatch:
         aLoop = false
      case aInfo := <-sTestClientId:
         NewLink(_newTestClient(eActCycle, aInfo))
      }
   }
   fmt.Fprintf(os.Stderr, " shutting down\n")
   Suspend()
   _ = os.RemoveAll(sStore.Root)
}

func _testMakeNode(iId, iN int) string {
   aNodeId, aNodeSha := makeNodeId()
   aSet := sTestNodeIds[iId]
   if aSet == nil {
      aSet = []string{"", "", ""}
   }
   aSet[iN] = aNodeId
   sTestNodeIds[iId] = aSet
   return aNodeSha
}

type tTestWork struct {
   Tmtp byte    // replay eOpTmtpRev object; other fields ignored
   Msg string   // send this; ignore Head & Data
   Head tMsg    // send this combined with Data or Datb
   Data string  // if set, ignore Datb
   Datb []byte
   Want []tMsg  // expected results, in order of sTestVerifyGot
   Nfsn bool    // sender's results not for sender's node
   wants string
}

var kOpSet = map[string]int{
   "eOpTmtpRev": eOpTmtpRev,
   "eOpRegister": eOpRegister, "eOpLogin": eOpLogin,
   "eOpUserEdit": eOpUserEdit, "eOpOhiEdit": eOpOhiEdit,
   "eOpGroupInvite": eOpGroupInvite, "eOpGroupEdit": eOpGroupEdit,
   "eOpPost": eOpPost, "eOpPostNotify": eOpPostNotify, "eOpPing": eOpPing,
   "eOpAck": eOpAck,
   "eOpPulse": eOpPulse, "eOpQuit": eOpQuit,
}

type tTestClient struct {
   id int // user id
   nodeN, nodeMax int // index and max-index of sTestNodeIds
   count int // msg number
   toRead, toWrite int // data yet to read/write
   action tTestAction // test mode
   ack chan string // writer tells reader to issue ack to qlib
   closed bool // when about to shut down
   readDeadline time.Time // set by qlib
}

type tTestAction int
const ( eActCycle tTestAction = iota; eActVerifySend; eActVerifyRecv )

func _newTestClient(iAct tTestAction, iInfo [3]int) *tTestClient {
   return &tTestClient{action: iAct, id: iInfo[0], nodeN: iInfo[1], nodeMax: iInfo[2],
                       ack: make(chan string, 10)}
}

func (o *tTestClient) Read(iBuf []byte) (int, error) {
   if o.action == eActCycle {
      return o._cycleRead(iBuf)
   }
   return o._verifyRead(iBuf)
}

func (o *tTestClient) _verifyRead(iBuf []byte) (int, error) {
   var aMsg []byte

   if o.action == eActVerifyRecv {
      o.count++
      if o.count == 1 {
         aMsg = packMsg(tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(o.id),
                             "Node":sTestNodeIds[o.id][o.nodeN]}, nil)
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
      time.Sleep(10 * time.Millisecond)
      select {
      case aId := <-o.ack:
         aMsg = packMsg(tMsg{"Op":eOpAck, "Id":aId, "Type":"n"}, nil)
         return copy(iBuf, aMsg), nil
      default:
      }
      aGot := strings.TrimSuffix(strings.Join(sTestVerifyGot[:], ""), "\n")
      if aGot != sTestVerifyWant.val {
         sTestVerifyFail++
         fmt.Fprintf(os.Stderr, "Verify FAIL:\n  want: %s\n  got:  %s\n", sTestVerifyWant.val, aGot)
      }
      for aK, aV := range sTestVerifyGotNode {
         aWant := sTestVerifyGot[2]
         if aK == o.id { // sender's node
            if sTestVerifyOp == eOpQuit || sTestVerifyOp == eOpOhiEdit && sTestVerifyNfsn {
               aWant = ""
            } else if sTestVerifyOp == eOpGroupInvite {
               aWant = sTestVerifyGot[2] + sTestVerifyGot[1]
            } else if sTestVerifyOp == eOpPostNotify {
               aWant = aWant[strings.LastIndexByte(aWant[:len(aWant)-1], '\n')+1:]
            } else if sTestVerifyGot[1] != "" && !sTestVerifyNfsn {
               aWant = sTestVerifyGot[1]
            }
         }
         aV, aWant = strings.TrimSuffix(aV, "\n"), strings.TrimSuffix(aWant, "\n")
         if aV != aWant {
            sTestVerifyFail++
            fmt.Fprintf(os.Stderr, "Verify node FAIL:\n  want: %s\n  got:  %s\n", aWant, aV)
         }
         sTestVerifyGotNode[aK] = ""
      }
      for a := range sTestVerifyGot { sTestVerifyGot[a] = "" }
      if o.count == len(sTestVerifyWork) {
         close(sTestVerifyDone)
         return 0, io.EOF
      }
      aWk := sTestVerifyWork[o.count]
      sTestVerifyWant.val = aWk.wants
      sTestVerifyOp, _ = aWk.Head["Op"].(int)
      sTestVerifyNfsn = aWk.Nfsn
      o.count++
      aMsg = []byte(aWk.Msg)
      if aWk.Msg == "" {
         aData := aWk.Datb; if aWk.Data != "" { aData = []byte(aWk.Data) }
         aMsg = packMsg(aWk.Head, aData)
      } else if aWk.Msg == "delay" {
         return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
      }
   }
   return copy(iBuf, aMsg), nil
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
         aHead = tMsg{"Op":eOpLogin, "Uid":"u"+fmt.Sprint(o.id), "Node":sTestNodeIds[o.id][o.nodeN]}
         o.nodeN++; if o.nodeN > o.nodeMax { o.nodeN = 0 }
         *sTestLogins[o.id]++
         _testLoginSummary()
      } else if o.count == 3 {
         type _tForOhi struct { Id string }
         aMax := int(sTestClientCount); if aMax > 100 { aMax = 100 }
         aFor := make([]_tForOhi, 0, aMax)
         for a := 0; a < aMax; a++ {
            aFor = append(aFor, _tForOhi{Id:"u"+fmt.Sprint(o.id/aMax*aMax+a)})
         }
         aHead = tMsg{"Op":eOpOhiEdit, "Id":fmt.Sprint(o.count), "For":aFor, "Type":"init"}
      } else if o.count == 4 && o.id % 2 == 1 {
         aData = []byte{0,0,0}
         aHead = tMsg{"Op":eOpPing, "Id":fmt.Sprint(o.count), "Datalen":len(aData),
                      "From":"a"+fmt.Sprint(o.id), "To":"a"+fmt.Sprint(o.id-1)}
      } else if o.nodeMax < 2 && aNs % 100 == 0 {
         o.count--
         o.nodeMax++
         aHead = tMsg{"Op":eOpUserEdit, "Id":fmt.Sprint(o.count), "Newnode":fmt.Sprint(o.nodeMax)}
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

func (o *tTestClient) Write(iBuf []byte) (int, error) {
   fCheckBuf := func(cBuf []byte) {
      if o.action != eActCycle {
         return
      }
      var cBad []byte
      for _, cB := range cBuf {
         if cB == 0 { continue }
         cBad = append(cBad, cB)
      }
      if len(cBad) > 0 {
         fmt.Fprintf(os.Stderr, "%d testclient.write data corruption: %s\n", o.id, string(cBad))
      }
   }

   if o.toWrite > 0 {
      if o.closed {
         return 0, &net.OpError{Op:"write", Err:tError("closed")}
      }
      fCheckBuf(iBuf)
      o.toWrite -= len(iBuf)
      return len(iBuf), nil
   }

   aHeadLen,_ := strconv.ParseInt(string(iBuf[:4]), 16, 0)
   var aHead tMsg
   err := json.Unmarshal(iBuf[4:aHeadLen+4], &aHead)
   if err != nil { panic(err) }

   aOp := aHead["op"].(string)
   if o.action >= eActVerifySend {
      aLine := string(iBuf[4:]) + "\n"
      if o.action == eActVerifyRecv && (aOp == "tmtprev" || aOp == "info" || aOp == "login") {
         // skip
      } else if o.nodeN > 0 {
         sTestVerifyGotNode[o.id] += aLine
      } else {
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
         sTestVerifyGot[aI] += aLine
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

      fCheckBuf(iBuf[aHeadLen+4:])
      aDatalen := int(aHead["datalen"].(float64))
      if o.action == eActCycle { _testRecvSummary(aDatalen) }
      o.toWrite = int(aHeadLen+4) + aDatalen - len(iBuf)

      if o.action == eActCycle && aOp == "user" && aHead["newnode"] != nil {
         aNodeN, _ := strconv.ParseInt(aHead["newnode"].(string), 0, 0)
         sTestNodeIds[o.id][aNodeN] = aHead["nodeid"].(string)
      }

      if !o.closed {
         aTmr := time.NewTimer(2 * time.Second)
         select {
         case o.ack <- aHead["id"].(string):
            aTmr.Stop()
         case <-aTmr.C:
            fmt.Fprintf(os.Stderr, "%d testclient.write timed out on ack\n", o.id)
            return 0, &net.OpError{Op:"write", Err:tError("noack")}
         }
      }
   }

   if o.closed {
      return 0, &net.OpError{Op:"write", Err:tError("closed")}
   }
   return len(iBuf), nil
}

func _testVerifyWantEdit(iOld, iNew string) {
   sTestVerifyWant.Lock()
   sTestVerifyWant.val = strings.Replace(sTestVerifyWant.val, "#"+iOld+"#", iNew, 1)
   sTestVerifyWant.Unlock()
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
         aTc := _newTestClient(eActVerifySend, [3]int{o.id, 0, 0})
         aTc.count = o.count
         time.AfterFunc(10*time.Millisecond, func(){ NewLink(aTc) })
      }
   } else {
      aSec := time.Now().Nanosecond() % 30 + 1
      time.AfterFunc(time.Duration(aSec) * time.Second, func(){
         sTestClientId <- [3]int{o.id, o.nodeN, o.nodeMax}
      })
   }
   return nil
}

func (o *tTestClient) LocalAddr()  net.Addr { return &net.UnixAddr{Name:"0.0.0.0:88"} }
func (o *tTestClient) RemoteAddr() net.Addr { return &net.UnixAddr{Name:"1.1.1.1:11"} }
func (o *tTestClient) SetDeadline(time.Time) error { return nil }
func (o *tTestClient) SetWriteDeadline(time.Time) error { return nil }

type tTimeoutError struct{}
func (o *tTimeoutError) Error() string   { return "i/o timeout" }
func (o *tTimeoutError) Timeout() bool   { return true }
func (o *tTimeoutError) Temporary() bool { return true }

