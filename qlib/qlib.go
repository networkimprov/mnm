// Copyright 2017, 2018 Liam Breck
// Published at https://github.com/networkimprov/mnm
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/

package qlib

import (
   "sync/atomic"
   "encoding/base32"
   "hash/crc32"
   "fmt"
   "io"
   "io/ioutil"
   "encoding/json"
   "net"
   "os"
   "crypto/rand"
   "crypto/sha1"
   "crypto/sha256"
   "sort"
   "strconv"
   "strings"
   "sync"
   "time"
   "unicode/utf8"
)

const kLoginTimeout time.Duration =  5 * time.Second
const kPulseTimeout time.Duration = 2 * time.Minute
const kQueueAckTimeout time.Duration = 30 * time.Second
const kQueueIdleMax time.Duration = 28 * time.Hour
const kStoreIdIncr = 1000
const kMsgHeaderMinLen = int64(len(`{"op":1}`))
const kMsgHeaderMaxLen = int64(1 << 16)
const kMsgPingDataMax = 140
const kNodeIdLen = 25
const kAliasMinLen = 8
const kPostDateFormat = "2006-01-02T15:04:05.000Z07:00"

var UDb UserDatabase // set by caller

// encoding without vowels to avoid words
var sBase32 = base32.NewEncoding("%+123456789BCDFGHJKLMNPQRSTVWXYZ")

var sCrc32c = crc32.MakeTable(crc32.Castagnoli)
var sSendDoor, sRecvDoor sync.RWMutex
var sOhi = tOhi{from: tOhiMap{}}
var sNode = tNodes{list: tNodeMap{}}
var sStore = tStore{}


type tHeader struct {
   Op uint8
   DataLen, DataHead int64
   DataSum uint64
   Uid, Gid string
   Id string
   Node, NewNode string
   NewAlias, From, To string // alias
   Type string
   Act string
   For []tHeaderFor
}

const (
   eOpTmtpRev = iota
   eOpRegister; eOpLogin
   eOpUserEdit; eOpOhiEdit;
   eOpGroupInvite; eOpGroupEdit
   eOpPost; eOpPing
   eOpAck
   eOpPulse; eOpQuit
   eOpEnd
)

type tHeaderFor struct { Id string; Type int8 }

const ( _=iota; eForUser; eForGroupAll; eForGroupExcl; eForSelf )

var sHeaderDefs = [...]tHeader{
   eOpTmtpRev    : { Id:"1" },
   eOpRegister   : { NewNode:"1", NewAlias:"1" },
   eOpLogin      : { Uid:"1", Node:"1" },
   eOpUserEdit   : { Id:"1" },
   eOpOhiEdit    : { Id:"1", For:[]tHeaderFor{{}}, Type:"1" },
   eOpGroupInvite: { Id:"1", DataLen:1, Gid:"1", From:"1", To:"1" },
   eOpGroupEdit  : { Id:"1", Act:"1", Gid:"1" },
   eOpPost       : { Id:"1", DataLen:1, For:[]tHeaderFor{{}} },
   eOpPing       : { Id:"1", DataLen:1, To:"1" },
   eOpAck        : { Id:"1", Type:"1" },
   eOpPulse      : {  },
   eOpQuit       : {  },
}

func (o *tHeader) check() bool {
   if o.Op >= eOpEnd { return false }
   aDef := &sHeaderDefs[o.Op]
   aFail :=
      o.DataLen < 0                                  ||
      o.DataLen < o.DataHead                         ||
      (aDef.DataLen == 0)    != (o.DataLen == 0)     ||
      aDef.DataSum       > 0 && o.DataSum       == 0 ||
      len(aDef.Uid)      > 0 && len(o.Uid)      == 0 ||
      len(aDef.Gid)      > 0 && len(o.Gid)      == 0 ||
      len(aDef.Id)       > 0 && len(o.Id)       == 0 ||
      len(aDef.Node)     > 0 && len(o.Node)     == 0 ||
      len(aDef.NewNode)  > 0 && len(o.NewNode)  == 0 ||
      len(aDef.NewAlias) > 0 && len(o.NewAlias) == 0 ||
      len(aDef.From)     > 0 && len(o.From)     == 0 ||
      len(aDef.To)       > 0 && len(o.To)       == 0 ||
      len(aDef.Type)     > 0 && len(o.Type)     == 0 ||
      len(aDef.Act)      > 0 && len(o.Act)      == 0 ||
      len(aDef.For)      > 0 && len(o.For)      == 0
   for _, aEl := range o.For {
      aFail = aFail || len(aEl.Id) == 0 ||
              o.Op == eOpPost && (aEl.Type < eForUser || aEl.Type >= eForSelf)
   }
   return !aFail
}


type tMsg map[string]interface{}

var sMsgOps = [...]string{
   eOpRegister:    "registered",
   eOpLogin:       "login",
   eOpUserEdit:    "user",
   eOpOhiEdit:     "ohiedit",
   eOpGroupInvite: "invite",
   eOpGroupEdit:   "member",
   eOpPost:        "delivery",
   eOpPing:        "ping",
   eOpEnd:         "",
}

type tMsgQuit struct {
   Error string `json:"error"`
   Op    string `json:"op"`
}

var (
   sMsgEof             = &tMsgQuit{Op:"eof"}
   sMsgTimeout         = &tMsgQuit{Op:"quit", Error:"connection timeout"}
   sMsgLengthBad       = &tMsgQuit{Op:"quit", Error:"invalid header length"}
   sMsgHeaderBad       = &tMsgQuit{Op:"quit", Error:"invalid header"}
   sMsgBase32Bad       = &tMsgQuit{Op:"quit", Error:"corrupt base32 value"}
   sMsgOpRedundant     = &tMsgQuit{Op:"quit", Error:"disallowed op repetition"}
   sMsgOpDisallowedOff = &tMsgQuit{Op:"quit", Error:"disallowed op on unauthenticated link"}
   sMsgOpDisallowedOn  = &tMsgQuit{Op:"quit", Error:"disallowed op on connected link"}
   sMsgNeedTmtpRev     = &tMsgQuit{Op:"quit", Error:"tmtprev was omitted"}
   sMsgRegisterFailure = &tMsgQuit{Op:"quit", Error:"register failure"} //todo details
   sMsgLoginFailure    = &tMsgQuit{Op:"quit", Error:"login failed"}
   sMsgLoginNodeOnline = &tMsgQuit{Op:"quit", Error:"node already connected"}
   sMsgLogout          = &tMsgQuit{Op:"quit", Error:"logout ok"}
   sMsgDatalenLimit    = &tMsgQuit{Op:"quit", Error:"data too long for request type"}
   sMsgDataNotUtf8     = &tMsgQuit{Op:"quit", Error:"data not valid UTF8"}
)

func msgConn(iErr net.Error) *tMsgQuit {
   if iErr.Timeout() { return sMsgTimeout }
   return &tMsgQuit{Op:"fail", Error:fmt.Sprintf("(tmp %v) %s", iErr.Temporary(), iErr.Error())}
}

func packMsg(iHead interface{}, iData []byte) []byte {
   aHead, err := json.Marshal(iHead)
   if err != nil { panic(err) }
   aLen := fmt.Sprintf("%04x", len(aHead))
   if len(aLen) != 4 { panic("header input too long") }
   aBuf := make([]byte, 0, 4+len(aHead)+len(iData))
   aBuf = append(aBuf, aLen...)
   aBuf = append(aBuf, aHead...)
   aBuf = append(aBuf, iData...)
   return aBuf
}


type tError string
func (o tError) Error() string { return string(o) }


type UserDatabase interface {
   // a UserDatabase stores:
   //   a set of Uids, one per user
   //   the set of Nodes for each user
   //   the set of Aliases for each user
   //   a set of Groups for message distribution
   //   the set of Aliases & Uids for each group

   AddUser(iUid, iNewNode string) (aQid string, err error)
   AddNode(iUid, iNewNode string) (aQid string, err error)
   DropNode(iUid, iNode string) (aQid string, err error)
   AddAlias(iUid, iNat, iEn string) error
   DropAlias(iUid, iAlias string) error
   //DropUser(iUid string) error

   Verify(iUid, iNode string) (aQid string, err error)
   OpenNodes(iUid string) (aQids []string, err error)
   CloseNodes(iUid string) error
   Lookup(iAlias string) (aUid string, err error)

   GroupInvite(iGid, iAlias, iByAlias, iByUid string) (aUid string, err error)
   GroupJoin(iGid, iUid, iNewAlias string) (aAlias string, err error)
   GroupAlias(iGid, iUid, iNewAlias string) (aAlias string, err error)
   GroupQuit(iGid, iAlias, iByUid string) (aUid string, err error)
   GroupGetUsers(iGid, iByUid string) (aUids []string, err error)

   // for test purposes
   TempUser(iUid, iNewNode string)
   TempAlias(iUid, iNewAlias string)
   TempGroup(iGid, iUid, iAlias string)
   Erase()
}


func Suspend() {
   sSendDoor.Lock()
   sRecvDoor.Lock()
}


type tLink struct { // network client msg handler
   conn net.Conn // link to client
   expectPulse bool
   queue *tQueue
   tmtprev string
   uid, node string
   ohi *tOhiSet
}

func NewLink(iConn net.Conn) {
   go _runLink(&tLink{conn:iConn})
}

func (o *tLink) Read(iBuf []byte) (int, error) {
   if o.expectPulse {
      err := o.conn.SetReadDeadline(time.Now().Add(kPulseTimeout))
      if err != nil { panic(err) }
   }
   return o.conn.Read(iBuf)
}

func _runLink(o *tLink) {
   aBuf := make([]byte, kMsgHeaderMaxLen+4) //todo start smaller, realloc as needed
   var aLen int
   var aPos, aHeadEnd int64
   var aQuitMsg *tMsgQuit

   err := o.conn.SetReadDeadline(time.Now().Add(kLoginTimeout))
   if err != nil { panic(err) }
   for {
      aLen, err = o.Read(aBuf[aPos:])
      if err != nil {
         if err == io.EOF {
            aQuitMsg = sMsgEof
         } else {
            //todo if recoverable continue
            aQuitMsg = msgConn(err.(net.Error))
         }
         break
      }
      aPos += int64(aLen)
   Parse:
      if aPos < kMsgHeaderMinLen+4 {
         continue
      }
      if aHeadEnd == 0 {
         aUi,_ := strconv.ParseUint(string(aBuf[:4]), 16, 0)
         aHeadEnd = int64(aUi)+4
         if aHeadEnd-4 < kMsgHeaderMinLen {
            aQuitMsg = sMsgLengthBad
            break
         }
      }
      if aHeadEnd > aPos {
         continue
      }
      aHead := &tHeader{Op:eOpEnd}
      err = json.Unmarshal(aBuf[4:aHeadEnd], aHead)
      if err != nil || !aHead.check() {
         aQuitMsg = sMsgHeaderBad
         break
      }
      aData := aBuf[aHeadEnd:aHeadEnd] // _checkPing may write into this
      if aPos > aHeadEnd && aHead.DataLen > 0 {
         aEnd := aHeadEnd + aHead.DataLen; if aPos < aEnd { aEnd = aPos }
         aData = aBuf[aHeadEnd:aEnd]
      }
      aQuitMsg = o._handleMsg(aHead, aData)
      if aQuitMsg != nil {
         break
      }
      if aPos > aHeadEnd + aHead.DataLen {
         aPos = int64(copy(aBuf, aBuf[aHeadEnd + aHead.DataLen : aPos]))
         aHeadEnd = 0
         goto Parse
      }
      aPos, aHeadEnd = 0,0
   }

   if aQuitMsg.Op == "eof" {
      fmt.Printf("%s link._runLink eof\n", o.uid)
   } else {
      fmt.Fprintf(os.Stderr, "%s link._runLink %s %s\n", o.uid, aQuitMsg.Op, aQuitMsg.Error)
      if aQuitMsg.Op == "quit" {
         o.conn.Write(packMsg(aQuitMsg, nil))
      }
   }
   o.conn.Close()
   if o.queue != nil {
      o.queue.unlink()
   }
   if o.ohi != nil {
      for _, aUid := range sOhi.unref(o.uid) {
         aNodes, err := UDb.OpenNodes(aUid)
         if err != nil {
            fmt.Fprintf(os.Stderr, "%s link._runLink opennodes %s\n", o.uid, err.Error())
            continue
         }
         o._sendOhi(aNodes, eOhiOff)
         _ = UDb.CloseNodes(aUid)
      }
   }
}

func (o *tLink) _handleMsg(iHead *tHeader, iData []byte) *tMsgQuit {
   sRecvDoor.RLock(); defer sRecvDoor.RUnlock()
   var err error
   var aMid, aPosted string

   switch iHead.Op {
   case eOpTmtpRev:
      if o.tmtprev != "" { return sMsgOpRedundant }
   case eOpRegister, eOpLogin:
      if o.tmtprev == "" { return sMsgNeedTmtpRev }
      if o.node    != "" { return sMsgOpDisallowedOn }
   default:
      if o.node    == "" { return sMsgOpDisallowedOff }
   }

   switch iHead.Op {
   case eOpTmtpRev:
      switch iHead.Id {
      case "1":
         o.tmtprev = iHead.Id
      default:
         o.tmtprev = "1"
      }
      o.conn.Write(packMsg(tMsg{"op":"tmtprev", "id":o.tmtprev}, nil))
   case eOpRegister:
      aUid := makeUid()
      aNodeId, aNodeSha := makeNodeId()
      _, err = UDb.AddUser(aUid, aNodeSha) //todo iHead.NewNode
      if err != nil {
         fmt.Fprintf(os.Stderr, "%s link._handleMsg register %s\n", o.uid, err.Error())
         return sMsgRegisterFailure
      }
      aAck := tMsg{"op":sMsgOps[iHead.Op], "uid":aUid, "nodeid":aNodeId}
      if iHead.NewAlias != "_" {
         if len(iHead.NewAlias) < kAliasMinLen { //todo enforce in userdb
            aAck["error"] = fmt.Sprintf("newalias must be %d+ characters", kAliasMinLen)
         } else {
            err = UDb.AddAlias(aUid, "", iHead.NewAlias)
            if err != nil {
               aAck["error"] = err.Error()
            }
         }
      }
      o.conn.Write(packMsg(aAck, nil))
      iHead.Uid = aUid
      iHead.Node = aNodeId
      fallthrough
   case eOpLogin:
      var aNodeSha, aQid string
      aNodeSha, err = getNodeSha(&iHead.Node)
      if err != nil {
         return sMsgBase32Bad
      }
      aQid, err = UDb.Verify(iHead.Uid, aNodeSha)
      if err != nil {
         return sMsgLoginFailure
      }
      aQ := queueLink(aQid, o.conn, tMsg{"op":"info", "info":"login ok", "ohi":nil}, iHead.Uid)
      if aQ == nil {
         return sMsgLoginNodeOnline
      }
      o.expectPulse = true
      o.uid = iHead.Uid
      o.node = aQid
      o.queue = aQ
      if iHead.Op != eOpRegister {
         iHead.For = []tHeaderFor{{Id:o.uid, Type:eForUser}}
         _, _, err = o._postMsg(iHead, tMsg{"node":"tbd"}, nil) //todo tbd=noderef
         if err != nil { panic(err) }
      }
      fmt.Printf("%s link._handleMsg login user %.7s\n", o.uid, aQ.node)
   case eOpUserEdit:
      if iHead.NewNode == "" && iHead.NewAlias == "" { return sMsgHeaderBad }
      if iHead.NewNode != "" && iHead.NewAlias != "" { return sMsgHeaderBad }
      aEtc := tMsg{}
      if iHead.NewAlias != "" {
         err = UDb.AddAlias(o.uid, "", iHead.NewAlias)
         if err == nil {
            aEtc["newalias"] = iHead.NewAlias
         }
      } else {
         aNodeId, aNodeSha := makeNodeId()
         var aQid string
         aQid, err = UDb.AddNode(o.uid, aNodeSha)
         if err == nil {
            err = sStore.copyDir(o.node, aQid)
            if err != nil { panic(err) }
            aEtc["nodeid"] = aNodeId
         }
      }
      if err == nil {
         iHead.For = []tHeaderFor{{Id:o.uid, Type:eForUser}}
         aMid, aPosted, err = o._postMsg(iHead, aEtc, nil)
      }
      if err != nil {
         fmt.Fprintf(os.Stderr, "%s link._handleMsg useredit %s\n", o.uid, err.Error())
      }
      o._ack(iHead.Id, aMid, aPosted, err)
   case eOpOhiEdit:
      if iHead.Type != "add" && iHead.Type != "drop" { return sMsgHeaderBad }
      for _, aTo := range iHead.For {
         _,err = UDb.OpenNodes(aTo.Id)
         if err != nil { break } //todo if err == defunct && Type == drop, continue
         _ = UDb.CloseNodes(aTo.Id)
      }
      if err == nil {
         aInit := o.ohi == nil
         if aInit {
            o.ohi = sOhi.ref(o.uid)
         }
         aStat := eOhiOff; if iHead.Type == "add" { aStat = eOhiOn }
         for _, aTo := range iHead.For {
            if o.ohi.edit(aTo.Id, iHead.Type == "add") {
               aNodes, aErr := UDb.OpenNodes(aTo.Id)
               if aErr == nil {
                  o._sendOhi(aNodes, aStat)
                  _ = UDb.CloseNodes(aTo.Id)
               }
            }
         }
         if !aInit {
            aHead := &tHeader{Op: eOpOhiEdit, For: []tHeaderFor{{Id:o.uid, Type:eForUser}}}
            aEtc := tMsg{"for":iHead.For, "type":iHead.Type}
            aMid, aPosted, err = o._postMsg(aHead, aEtc, nil)
         }
      }
      o._ack(iHead.Id, aMid, aPosted, err)
   case eOpGroupInvite:
      iHead.Act = "invite"
      fallthrough
   case eOpGroupEdit:
      var aUid, aAlias, aNewAlias string
      switch iHead.Act {
      case "invite":
         aQuitMsg := o._checkPing(iHead, &iData)
         if aQuitMsg != nil { return aQuitMsg }
         aUid, err = UDb.GroupInvite(iHead.Gid, iHead.To, iHead.From, o.uid)
         if err == nil {
            iHead.For = []tHeaderFor{{Id:aUid, Type:eForUser}}
            _, _, err = o._postMsg(iHead, tMsg{"gid":iHead.Gid, "to":iHead.To}, iData)
            aAlias = iHead.To
         }
      case "join":
         aAlias, err = UDb.GroupJoin(iHead.Gid, o.uid, iHead.NewAlias)
      case "alias":
         if iHead.NewAlias == "" { return sMsgHeaderBad }
         aAlias, err = UDb.GroupAlias(iHead.Gid, o.uid, iHead.NewAlias)
         aNewAlias = iHead.NewAlias
      case "drop":
         if iHead.To == "" { return sMsgHeaderBad }
         aUid, err = UDb.GroupQuit(iHead.Gid, iHead.To, o.uid)
         aAlias = iHead.To
      default:
         return sMsgHeaderBad
      }
      if err == nil {
         aEtc := tMsg{"gid":iHead.Gid, "act":iHead.Act, "alias":aAlias}
         if aNewAlias != "" {
            aEtc["newalias"] = aNewAlias
         }
         aHead := &tHeader{Op: eOpGroupEdit, For: []tHeaderFor{{Id:iHead.Gid, Type:eForGroupAll}}}
         aMid, aPosted, err = o._postMsg(aHead, aEtc, nil)
      }
      if err != nil {
         fmt.Fprintf(os.Stderr, "%s link._handleMsg group %s\n", o.uid, err.Error())
      }
      o._ack(iHead.Id, aMid, aPosted, err)
   case eOpPost:
      aMid, aPosted, err = o._postMsg(iHead, nil, iData)
      if err != nil {
         if err == io.EOF { return sMsgEof }
         if aErr, _ := err.(net.Error); aErr != nil { return msgConn(aErr) }
         fmt.Fprintf(os.Stderr, "%s link._handleMsg post %s\n", o.uid, err.Error())
      }
      o._ack(iHead.Id, aMid, aPosted, err)
   case eOpPing:
      aQuitMsg := o._checkPing(iHead, &iData)
      if aQuitMsg != nil { return aQuitMsg }
      var aUid string
      aUid, err = UDb.Lookup(iHead.To)
      if err == nil {
         iHead.For = []tHeaderFor{{Id:aUid, Type:eForUser}}
         aMid, aPosted, err = o._postMsg(iHead, tMsg{"to":iHead.To}, iData)
      }
      if err != nil {
         fmt.Fprintf(os.Stderr, "%s link._handleMsg ping %s\n", o.uid, err.Error())
      }
      o._ack(iHead.Id, aMid, aPosted, err)
   case eOpAck:
      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.queue.ack <- iHead.Id:
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Fprintf(os.Stderr, "%s link._handleMsg timed out waiting on ack\n", o.uid)
      }
   case eOpPulse:
      // no-op
   case eOpQuit:
      return sMsgLogout
   default:
      panic(fmt.Sprintf("checkHeader failure, op %d", iHead.Op))
   }
   return nil
}

func (o *tLink) _checkPing(iHead *tHeader, iData *[]byte) *tMsgQuit {
   if iHead.DataLen > kMsgPingDataMax {
      return sMsgDatalenLimit
   }
   for len(*iData) < int(iHead.DataLen) {
      aLen, err := o.Read((*iData)[len(*iData):iHead.DataLen]) // panics if cap() < DataLen
      if err != nil {
         if err == io.EOF { return sMsgEof }
         return msgConn(err.(net.Error))
      }
      *iData = (*iData)[:len(*iData)+aLen]
   }
   if !utf8.Valid(*iData) {
      return sMsgDataNotUtf8
   }
   return nil
}

func (o *tLink) _sendOhi(iNodes []string, iStat int8) {
   for _, aNid := range iNodes {
      aNd := getNode(aNid)
      aNd.RLock()
      if aNd.queue != nil {
         aTmr := time.NewTimer(200 * time.Millisecond)
         select {
         case aNd.queue.ohi <- tOhiMsg{from:o.uid, status:iStat}:
            aTmr.Stop()
         case <-aTmr.C:
            fmt.Fprintf(os.Stderr, "%s link._sendOhi timeout node %s\n", o.uid, aNid)
         }
      }
      aNd.RUnlock()
   }
}

func (o *tLink) _ack(iId, iMsgId, iPosted string, iErr error) {
   aMsg := tMsg{"op":"ack", "id":iId, "msgid":iMsgId, "posted":iPosted}
   if iErr != nil {
      aMsg["error"] = iErr.Error()
   }
   o.conn.Write(packMsg(aMsg, nil))
}

func (o *tLink) _postMsg(iHead *tHeader, iEtc tMsg, iData []byte) (aMsgId, aPosted string, err error) {
   aMsgId = sStore.makeId()
   aPosted = time.Now().UTC().Format(kPostDateFormat)
   aHead := tMsg{"op":sMsgOps[iHead.Op], "id":aMsgId, "from":o.uid, "datalen":iHead.DataLen,
                 "posted":aPosted}
   //todo insert "datalen" if != 0
   if iHead.DataHead != 0 {
      aHead["datahead"] = iHead.DataHead
   }
   if iHead.DataSum != 0 {
      aHead["datasum"] = iHead.DataSum
   }
   if iEtc != nil {
      for aK, aV := range iEtc { aHead[aK] = aV }
   }
   aHead["headsum"] = crc32.Checksum(packMsg(aHead, nil), sCrc32c)

   err = sStore.recvFile(aMsgId, packMsg(aHead, nil), iData, o, iHead.DataLen)
   if err != nil {
      if _, ok := err.(net.Error); !ok && err != io.EOF { panic(err) }
   }
   defer sStore.rmFile(aMsgId)
   if err != nil { return "", "", err }

   aForNodes := make(map[string]bool, len(iHead.For)) //todo x2 or more?
   aForMyUid := false
   iHead.For = append(iHead.For, tHeaderFor{Id:o.uid, Type:eForSelf})

   for _, aTo := range iHead.For {
      var aUids []string
      switch aTo.Type {
      case eForGroupAll, eForGroupExcl:
         aUids, err = UDb.GroupGetUsers(aTo.Id, o.uid)
         if err != nil { return "", "", err }
      default:
         aUids = []string{aTo.Id}
      }
      for _, aUid := range aUids {
         if aTo.Type == eForGroupExcl && aUid == o.uid {
            continue
         }
         var aNodes []string
         aNodes, err = UDb.OpenNodes(aUid)
         if err != nil { return "", "", err }
         defer UDb.CloseNodes(aUid)
         for _, aNd := range aNodes {
            aForNodes[aNd] = true
         }
         aForMyUid = aForMyUid || aUid == o.uid && aTo.Type != eForSelf
      }
   }
   for aNodeId,_ := range aForNodes {
      if aNodeId == o.node && !aForMyUid {
         continue
      }
      aNd := getNode(aNodeId)
      aNd.RLock()
      err = sStore.putLink(aMsgId, aNodeId, aMsgId)
      if err != nil { panic(err) }
      err = sStore.syncDirs(aNodeId)
      if err != nil { panic(err) }
      if aNd.queue != nil {
         aNd.queue.in <- aMsgId
      }
      aNd.RUnlock()
   }
   return aMsgId, aPosted, nil
}


type tOhi struct {
   from tOhiMap // users notifying others of presence
   sync.RWMutex
}

type tOhiMap map[string]*tOhiSet // indexed by uid

type tOhiMsg struct {
   from string
   status int8
}

const ( _ int8 = iota; eOhiOn; eOhiOff; )

type tOhiSet struct {
   uid map[string]bool // users to notify
   sync.RWMutex
   refcount int32 // online nodes
}

func (o *tOhiSet) edit(iTo string, iNew bool) bool {
   o.Lock()
   aOld := o.uid[iTo]
   o.uid[iTo] = iNew
   o.Unlock()
   return aOld != iNew
}

func (o *tOhi) ref(iFrom string) *tOhiSet {
   o.RLock()
   aSet := o.from[iFrom]
   if aSet != nil {
      atomic.AddInt32(&aSet.refcount, 1)
   }
   o.RUnlock()

   if aSet == nil {
      o.Lock()
      if aTemp := o.from[iFrom]; aTemp != nil {
         aSet = aTemp
         aSet.refcount++
      } else {
         aSet = &tOhiSet{refcount:1, uid:make(map[string]bool)}
         o.from[iFrom] = aSet
      }
      o.Unlock()
   }
   return aSet
}

func (o *tOhi) unref(iFrom string) []string {
   o.RLock()
   aSet := o.from[iFrom]
   aN := atomic.AddInt32(&aSet.refcount, -1) // crash if from[iFrom] not found
   o.RUnlock()

   var aList []string
   if aN == 0 {
      o.Lock()
      if aSet.refcount == 0 {
         delete(o.from, iFrom)
         for aK, aV := range aSet.uid {
            if aV { aList = append(aList, aK) }
         }
      }
      o.Unlock()
   }
   return aList
}

func (o *tOhi) getOhiTo(iUid string) []string {
   var aSet []string
   o.RLock()
   for aK, aV := range o.from {
      aV.RLock()
      if aV.uid[iUid] {
         aSet = append(aSet, aK)
      }
      aV.RUnlock()
   }
   o.RUnlock()
   return aSet
}


type tNodes struct {
   list tNodeMap // nodes that have received msgs or loggedin
   sync.RWMutex //todo Mutex when sync.map
}

type tNodeMap map[string]*tNode // indexed by node id

type tNode struct {
   sync.RWMutex // directory lock
   queue *tQueue // instantiated on login //todo free on idle
}

func getNode(iNode string) *tNode {
   sNode.RLock() //todo drop for sync.map
   aNd := sNode.list[iNode]
   sNode.RUnlock()
   if aNd != nil {
      return aNd
   }
   sNode.Lock()
   aNd = sNode.list[iNode]
   if aNd == nil {
      fmt.Printf("%.7s getnode make node\n", iNode)
      aNd = new(tNode)
      sNode.list[iNode] = aNd
   }
   sNode.Unlock()
   return aNd
}


type tQueue struct {
   node string
   connChan chan net.Conn // control access to conn
   hasConn int32 // in use by tLink
   ack chan string // forwards acks from client
   buf []string // elastic channel buffer
   in chan string // elastic channel input
   out chan string // elastic channel output
   ohi chan tOhiMsg // presence notifications to us
}

func queueLink(iNode string, iConn net.Conn, iMsg tMsg, iUid string) *tQueue {
   aNd := getNode(iNode)
   if aNd.queue == nil {
      aNd.Lock()
      if aNd.queue != nil {
         aNd.Unlock()
         fmt.Fprintf(os.Stderr, "%.7s newqueue attempt to recreate queue\n", iNode)
      } else {
         aNd.queue = new(tQueue)
         aQ := aNd.queue
         aQ.node = iNode
         aQ.connChan = make(chan net.Conn, 1)
         aQ.ack = make(chan string, 10)
         aQ.in = make(chan string)
         aQ.out = make(chan string)
         aQ.ohi = make(chan tOhiMsg, 100) //todo tune size
         var err error
         aQ.buf, err = sStore.getDir(iNode)
         if err != nil { panic(err) }
         aNd.Unlock()
         fmt.Printf("%.7s newqueue create queue\n", iNode)
         go _runElasticChan(aQ)
         go _runQueue(aQ)
      }
   }
   if !atomic.CompareAndSwapInt32(&aNd.queue.hasConn, 0, 1) {
      return nil
   }
   aOhi := sOhi.getOhiTo(iUid)
   if len(aOhi) > 0 {
      iMsg["ohi"] = aOhi
   } else {
      delete(iMsg, "ohi")
   }
   iConn.Write(packMsg(iMsg, nil))
   aNd.queue.connChan <- iConn
   return aNd.queue
}

func (o *tQueue) unlink() {
   <-o.connChan
   o.hasConn = 0
}

func (o *tQueue) _waitForMsg() string {
   for {
      select {
      case aMid := <-o.out:
         return aMid
      case aOhi := <-o.ohi:
         o._tryOhi(&aOhi)
      }
   }
}

func (o *tQueue) _tryOhi(iOhi *tOhiMsg) {
   aMsg := packMsg(tMsg{"op":"ohi", "from":iOhi.from, "status":iOhi.status}, nil)
   select {
   case aConn := <-o.connChan:
      o.connChan <- aConn
      _,err := aConn.Write(aMsg)
      if err != nil {
         fmt.Fprintf(os.Stderr, "%.7s queue._tryOhi write error %s\n", o.node, err.Error())
      }
   default: // drop msg
   }
}

func _runQueue(o *tQueue) {
   aMsgId := o._waitForMsg()
   for {
      aConn := <-o.connChan
      o.connChan <- aConn
      sSendDoor.RLock()
   SendFile:
      err := sStore.sendFile(o.node, aMsgId, aConn)
      if err != nil {
         sSendDoor.RUnlock()
         if _, ok := err.(*os.PathError); ok { panic(err) } //todo move to sStore?
         //todo recoverable?
         fmt.Fprintf(os.Stderr, "%.7s queue._runQueue sendfile error %s\n", o.node, err.Error())
         continue
      }
      aTimeout := time.NewTimer(kQueueAckTimeout)
   RecvAck:
      select {
      case aOhi := <-o.ohi:
         o._tryOhi(&aOhi)
         goto RecvAck
      case aAckId := <-o.ack:
         aTimeout.Stop()
         if aAckId != aMsgId {
            fmt.Fprintf(os.Stderr, "%.7s queue._runQueue ack got %s, want %s\n", o.node, aAckId, aMsgId)
            goto SendFile
         }
         sStore.rmLink(o.node, aMsgId)
         sSendDoor.RUnlock()
         aMsgId = o._waitForMsg()
      case <-aTimeout.C:
         sSendDoor.RUnlock()
         fmt.Fprintf(os.Stderr, "%.7s queue._runQueue timed out awaiting ack\n", o.node)
      }
   }
}

func _runElasticChan(o *tQueue) {
   var aS string
   var ok bool
   for {
      // buf needs a value to let select multiplex consumer & producer
      if len(o.buf) == 0 {
         aS, ok = <-o.in
         if !ok { goto closed }
         o.buf = append(o.buf, aS)
      }

      select {
      case aS, ok = <-o.in:
         if !ok { goto closed }
         o.buf = append(o.buf, aS)
         if len(o.buf) % 100 == 0 {
            fmt.Fprintf(os.Stderr, "%.7s queue._runElasticChan buf len %d\n", o.node, len(o.buf))
         }
      case o.out <- o.buf[0]:
         o.buf = o.buf[1:]
      }
   }

closed:
   for _, aS = range o.buf {
      o.out <- aS
   }
   close(o.out)
}


func makeUid() string {
   aT := time.Now()
   aSeed := fmt.Sprintf("%s%00d%000000000d", sStore.makeId(), aT.Second(), aT.Nanosecond())
   aData := sha1.Sum([]byte(aSeed))
   return sBase32.EncodeToString(aData[:])
}

func makeNodeId() (aNodeId, aSha string) {
   aData := make([]byte, kNodeIdLen)
   _, err := rand.Read(aData)
   if err != nil { panic(err) }
   aNodeId = sBase32.EncodeToString(aData)
   aSha = _node2sha(aData)
   return aNodeId, aSha
}

func getNodeSha(iNode *string) (string, error) {
   aData, err := sBase32.DecodeString(*iNode)
   if err != nil { return "", err }
   aSha := _node2sha(aData)
   *iNode = "" //todo erase the internal array?
   return aSha, nil
}

func _node2sha(iNode []byte) string {
   aData := sha256.Sum256(iNode)
   for a:=0; a < 22388; a++ { //todo per-user count?
      aData = sha256.Sum256(aData[:]) //todo alternate algorithm
   }
   aText := sBase32.EncodeToString(aData[:])
   if aText[len(aText)-4] != '=' { panic("padding less than 4") } //todo temp
   return aText[:len(aText)-4] // omit padding
}


type tStore struct { // queue and msg storage
   Root string // top-level directory
   temp string // msg files land here before hardlinks land in queue directories
   nextId uint64 // incrementing msg filename
   idStore chan uint64 // updates nextId on disk
}

func Init(iMain string) {
   o := &sStore
   o.Root = iMain + "/"
   o.temp = o.Root + "temp/"
   o.idStore = make(chan uint64, 1)

   err := os.MkdirAll(o.temp, 0700)
   if err != nil { panic(err) }

   var aWg sync.WaitGroup
   aWg.Add(1)
   go _runIdStore(o, &aWg)
   aWg.Wait()
}

func _runIdStore(o *tStore, iWg *sync.WaitGroup) {
   aBuf, err := ioutil.ReadFile(o.Root+"NEXTID")
   if err != nil {
      if !os.IsNotExist(err) { panic(err) }
      aBuf = make([]byte, 16)
   } else {
      o.nextId, err = strconv.ParseUint(string(aBuf), 16, 64)
      if err != nil { panic(err) }
   }
   o.idStore <- o.nextId

   aFd, err := os.OpenFile(o.Root+"NEXTID", os.O_WRONLY|os.O_CREATE, 0600)
   if err != nil { panic(err) }
   defer aFd.Close()

   for {
      aId := <-o.idStore + (2 * kStoreIdIncr)
      copy(aBuf, fmt.Sprintf("%016x", aId))

      _, err = aFd.Seek(0, 0)
      if err != nil { panic(err) }

      _, err = aFd.Write(aBuf)
      if err != nil { panic (err) }

      err = aFd.Sync()
      if err != nil { panic (err) }

      if iWg != nil {
         iWg.Done()
         iWg = nil
      }
   }
}

func (o *tStore) makeId() string {
   aN := atomic.AddUint64(&o.nextId, 1)
   if aN % 1000 == 0 {
      o.idStore <- aN
   }
   return fmt.Sprintf("%016x", aN)
}

func (o *tStore) recvFile(iId string, iHead, iData []byte, iStream io.Reader, iLen int64) error {
   aFd, err := os.OpenFile(o.temp+iId, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
   if err != nil { return err }
   defer aFd.Close()
   _,err = aFd.Write(iHead)
   if err != nil { return err }
   for aPos, aLen := 0,0; aPos < len(iData); aPos += aLen {
      aLen, err = aFd.Write(iData[aPos:])
      if err != nil && err != io.ErrShortWrite { return err }
   }
   _,err = io.CopyN(aFd, iStream, iLen - int64(len(iData)))
   if err != nil { return err }
   err = aFd.Sync()
   return err
}

func (o *tStore) zeroFile(iNode, iId string) error {
   aFd, err := os.OpenFile(o._nodeSub(iNode)+"/"+iId, os.O_WRONLY|os.O_TRUNC, 0600)
   if err != nil { return err }
   aFd.Close()
   return nil
}

func (o *tStore) putLink(iSrc, iNode, iId string) error {
   aPath := o._nodeSub(iNode)
   err := os.MkdirAll(aPath, 0700)
   if err != nil { return err }
   err = os.Link(o.temp+iSrc, aPath+"/"+iId)
   return err
}

func (o *tStore) rmFile(iId string) error {
   return os.Remove(o.temp+iId)
}

func (o *tStore) rmLink(iNode, iId string) error {
   return os.Remove(o._nodeSub(iNode)+"/"+iId)
}

func (o *tStore) rmDir(iNode string) error {
   err := os.Remove(o._nodeSub(iNode))
   if os.IsNotExist(err) { return nil }
   return err
}

func (o *tStore) syncDirs(iNode string) error {
   var aFd *os.File
   var err error
   fSync := func(aDir string) {
      aFd, err = os.Open(aDir)
      if err != nil { return }
      err = aFd.Sync()
      aFd.Close()
   }
   fSync(o.Root)
   if err != nil { return err }
   fSync(o._rootSub(iNode))
   if err != nil { return err }
   fSync(o._nodeSub(iNode))
   return err
}

func (o *tStore) sendFile(iNode, iId string, iConn net.Conn) error {
   aFd, err := os.Open(o._nodeSub(iNode)+"/"+iId)
   if err != nil { return err }
   defer aFd.Close()
   _,err = io.Copy(iConn, aFd) // calls sendfile(2) in iConn.ReadFrom()
   return err
}

func (o *tStore) getDir(iNode string) (ret []string, err error) {
   fmt.Printf("read dir %s\n", o._nodeSub(iNode))
   aFd, err := os.Open(o._nodeSub(iNode))
   if err != nil {
      if os.IsNotExist(err) { err = nil }
      return
   }
   ret, err = aFd.Readdirnames(0)
   sort.Slice(ret, func(i, j int) bool { return ret[i] < ret[j] })
   aFd.Close()
   return
}

func (o *tStore) copyDir(iNode, iToNode string) error {
   aDirs, err := o.getDir(iNode)
   if err != nil { return err }
   if len(aDirs) == 0 {
      return nil
   }
   os.MkdirAll(o._nodeSub(iToNode), 0700)
   for _, aId := range aDirs {
      err = os.Link(o._nodeSub(iNode)+"/"+aId, o._nodeSub(iToNode)+"/"+aId)
      if err != nil && !os.IsNotExist(err) && !os.IsExist(err) { return err }
   }
   return nil
}

func (o *tStore) _rootSub(iNode string) string {
   return o.Root + strings.ToLower(iNode[:4])
}

func (o *tStore) _nodeSub(iNode string) string {
   return o._rootSub(iNode) + "/" + strings.ToLower(iNode)
}

