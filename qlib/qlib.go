package qlib

import (
   "sync/atomic"
   "fmt"
   "io"
   "io/ioutil"
   "encoding/json"
   "net"
   "os"
   "sort"
   "strconv"
   "strings"
   "sync"
   "time"
)

const kLoginTimeout time.Duration =  5 * time.Second
const kQueueAckTimeout time.Duration = 3 * time.Second
const kQueueIdleMax time.Duration = 28 * time.Hour
const kStoreIdIncr = 1000
const kMsgHeaderMinLen = len(`{"op":1}`)
const kMsgHeaderMaxLen = 1 << 8 //todo larger?

const ( _=iota; eRegister; eAddNode; eLogin; eListEdit; ePost; ePing; eAck; eOpEnd )

const ( _=iota; eForUser; eForGroupAll; eForGroupExcl; eForSelf )

var sHeaderDefs = [...]tHeader{
   eRegister: { Uid:"1", NewNode:"1", Aliases:"1"    },
   eAddNode : { Uid:"1", NodeId:"1", NewNode:"1"     },
   eLogin   : { Uid:"1", NodeId:"1"                  },
   eListEdit: { Id:"1", To:"1", Type:"1", Member:"1" },
   ePost    : { Id:"1", For:[]tHeaderFor{{}}         },
   ePing    : { Id:"1", Alias:"1"                    },
   eAck     : { Id:"1", Type:"1"                     },
}

var sResponseOps = [...]string{
   eRegister: "registered",
   eAddNode:  "nodeAdded",
   eListEdit: "listEdited",
   ePost:     "delivery",
   ePing:     "pong",
   eOpEnd:    "",
}

var (
   sMsgIncomplete      = tMsg{"op":"quit", "info":"incomplete header"}
   sMsgLengthBad       = tMsg{"op":"quit", "info":"invalid header length"}
   sMsgHeaderBad       = tMsg{"op":"quit", "info":"invalid header"}
   sMsgOpDisallowed    = tMsg{"op":"quit", "info":"disallowed op on unauthenticated link"}
   sMsgOpDataless      = tMsg{"op":"quit", "info":"op does not support data"}
   sMsgLoginTimeout    = tMsg{"op":"quit", "info":"login timeout"}
   sMsgLoginFailure    = tMsg{"op":"quit", "info":"login failed"}
   sMsgLoginNodeOnline = tMsg{"op":"quit", "info":"node already connected"}
   sMsgLogin           = tMsg{"op":"info", "info":"login ok"}
)

var sNode = tNodes{list: tNodeMap{}}
var sStore = tStore{}
var UDb UserDatabase // set by caller


type UserDatabase interface {
   // a UserDatabase stores:
   //   a set of Uids, one per user
   //   the set of Nodes for each user
   //   the set of Aliases for each user
   //   a set of message distribution groups
   //   the set of Uids for each group

   AddUser(iUid, iNewNode string, iAliases []string) (aAliases []string, err error)
   SetAliases(iUid, iNode string, iAliases []string) (aAliases []string, err error)
   AddNode(iUid, iNode, iNewNode string) (aNodeRef int, err error)
   DropNode(iUid, iNode string) error
   //DropUser(iUid string) error

   Verify(iUid, iNode string) (aNodeRef int, err error)
   GetNodes(iUid string) (aNodes []string, err error)
   Lookup(iAlias string) (aUid string, err error)

   GroupInvite(iGid, iBy, iAlias string) error
   GroupJoin(iGid, iAlias, iUid string) (aAlias string, err error)
   GroupDrop(iGid, iBy, iUid string) error
   GroupLookup(iGid, iBy string) (aUids []string, err error)
}


type Link struct { // network client msg handler
   conn net.Conn // link to client
   queue *tQueue
   uid, node string
}

func NewLink(iConn net.Conn) *Link {
   aL := &Link{conn:iConn}
   go runLink(aL)
   return aL
}

func runLink(o *Link) {
   var aQuitMsg tMsg
   aBuf := make([]byte, kMsgHeaderMaxLen+4)

   o.conn.SetReadDeadline(time.Now().Add(kLoginTimeout))
   for {
      aLen, err := o.conn.Read(aBuf)
      if err != nil {
         //todo if recoverable continue
         if err.(net.Error).Timeout() {
            aQuitMsg = sMsgLoginTimeout
         } else {
            fmt.Printf("%s link.runlink net error %s\n", o.uid, err.Error())
         }
         break
      }
      if aLen < kMsgHeaderMinLen+4 {
         aQuitMsg = sMsgIncomplete
         break
      }
      aUi,_ := strconv.ParseUint(string(aBuf[:4]), 16, 0)
      aHeadEnd := int(aUi)+4
      if aHeadEnd-4 < kMsgHeaderMinLen || aHeadEnd > aLen {
         aQuitMsg = sMsgLengthBad
         break
      }
      aHead := new(tHeader)
      err = json.Unmarshal(aBuf[4:aHeadEnd], aHead)
      if err != nil || !aHead.check() {
         aQuitMsg = sMsgHeaderBad
         break
      }
      var aData []byte
      if aLen > aHeadEnd {
         aData = aBuf[aHeadEnd:aLen]
      }
      aQuitMsg = o.HandleMsg(aHead, aData)
      if aQuitMsg != nil { break }
   }

   if aQuitMsg != nil {
      fmt.Printf("%s link.runlink quit %s\n", o.uid, aQuitMsg["info"].(string))
      o.conn.Write(PackMsg(aQuitMsg, nil))
   }
   o.conn.Close()
   if o.queue != nil {
      o.queue.Unlink()
   }
}

type tHeader struct {
   Op uint8
   Uid string
   Id string
   NodeId, NewNode string
   Aliases string
   To string
   Type string
   Member string
   Alias string
   For []tHeaderFor
}

type tHeaderFor struct { Id string; Type int8 }

func (o *tHeader) check() bool {
   if o.Op == 0 || o.Op >= eOpEnd { return false }
   aDef := &sHeaderDefs[o.Op]
   aFail :=
      len(aDef.Uid)     > 0 && len(o.Uid)     == 0 ||
      len(aDef.Id)      > 0 && len(o.Id)      == 0 ||
      len(aDef.NodeId)  > 0 && len(o.NodeId)  == 0 ||
      len(aDef.NewNode) > 0 && len(o.NewNode) == 0 ||
      len(aDef.Aliases) > 0 && len(o.Aliases) == 0 ||
      len(aDef.To)      > 0 && len(o.To)      == 0 ||
      len(aDef.Type)    > 0 && len(o.Type)    == 0 ||
      len(aDef.Member)  > 0 && len(o.Member)  == 0 ||
      len(aDef.Alias)   > 0 && len(o.Alias)   == 0 ||
      len(aDef.For)     > 0 && len(o.For)     == 0
   return !aFail
}

func (o *Link) HandleMsg(iHead *tHeader, iData []byte) tMsg {
   var err error

   if iHead.Op != eRegister && iHead.Op != eAddNode && iHead.Op != eLogin {
      if o.node == "" { return sMsgOpDisallowed }
   }

   if iHead.Op != ePost && iHead.Op != eListEdit && iHead.Op != ePing {
      if iData != nil { return sMsgOpDataless }
   }

   switch(iHead.Op) {
   case eLogin:
      _, err = UDb.Verify(iHead.Uid, iHead.NodeId)
      if err != nil {
         return sMsgLoginFailure
      }
      aQ := QueueLink(iHead.NodeId, o.conn)
      if aQ == nil {
         return sMsgLoginNodeOnline
      }
      o.conn.SetReadDeadline(time.Time{})
      o.uid = iHead.Uid
      o.node = iHead.NodeId
      o.queue = aQ
      fmt.Printf("%s link.handlemsg login user %s\n", o.uid, aQ.uid)
   case ePost:
      aMsgId := sStore.MakeId()
      aBuf := PackMsg(tMsg{"Op":sResponseOps[ePost], "Id":aMsgId, "From":o.uid}, iData)
      err = sStore.PutFile(aMsgId, aBuf)
      if err != nil { panic(err) }
      aForNodes := make(map[string]bool, len(iHead.For)) //todo x2 or more?
      aForMyUid := false
      iHead.For = append(iHead.For, tHeaderFor{Id:o.uid, Type:eForSelf})
      for _, aTo := range iHead.For {
         var aUids []string
         switch (aTo.Type) {
         case eForGroupAll, eForGroupExcl:
            aUids, err = UDb.GroupLookup(aTo.Id, o.uid)
            if err != nil { panic(err) }
         default:
            aUids = []string{aTo.Id}
         }
         for _, aUid := range aUids {
            if aTo.Type == eForGroupExcl && aUid == o.uid {
               continue
            }
            aNodes, err := UDb.GetNodes(aUid)
            if err != nil { panic(err) }
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
         aNd := GetNode(aNodeId)
         aNd.dir.RLock()
         sStore.PutLink(aMsgId, aNodeId, aMsgId)
         sStore.SyncDirs(aNodeId)
         if aNd.queue != nil {
            aNd.queue.in <- aMsgId
         }
         aNd.dir.RUnlock()
      }
      o.conn.Write(PackMsg(tMsg{"op:":"ack", "id":iHead.Id, "type":"ok"}, nil))
      sStore.RmFile(aMsgId)
   case eAck:
      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.queue.ack <- iHead.Id:
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Printf("%s link.handlemsg timed out waiting on ack\n", o.uid)
      }
   default:
      panic(fmt.Sprintf("checkHeader failure, op %d", iHead.Op))
   }
   return nil
}

type tMsg map[string]interface{}

func PackMsg(iJso tMsg, iData []byte) []byte {
   var err error
   var aEtc []byte
   aEtcdata := iJso["etcdata"]
   if aEtcdata != nil {
      delete(iJso, "etcdata")
      aEtc, err = json.Marshal(aEtcdata)
      if err != nil { panic(err) }
      iJso["etc"] = len(aEtc)
   }
   aReq, err := json.Marshal(iJso)
   if err != nil { panic(err) }
   aLen := fmt.Sprintf("%04x", len(aReq))
   if len(aLen) > 4 { panic("packmsg json input too long") }
   aBuf := make([]byte, 0, len(aLen)+len(aReq)+len(aEtc)+len(iData))
   aBuf = append(aBuf, aLen...)
   aBuf = append(aBuf, aReq...)
   aBuf = append(aBuf, aEtc...)
   aBuf = append(aBuf, iData...)
   return aBuf
}


type tNodes struct {
   list tNodeMap // nodes that have received msgs or loggedin
   create sync.RWMutex //todo Mutex when sync.map
}

type tNodeMap map[string]*tNode // indexed by node id

type tNode struct {
   dir sync.RWMutex // directory lock
   queue *tQueue // instantiated on login //todo free on idle
}

func GetNode(iUid string) *tNode {
   sNode.create.RLock() //todo drop for sync.map
   aNd := sNode.list[iUid]
   sNode.create.RUnlock()
   if aNd != nil {
      return aNd
   }
   sNode.create.Lock()
   aNd = sNode.list[iUid]
   if aNd == nil {
      fmt.Printf("%s getnode make node\n", iUid)
      aNd = new(tNode)
      sNode.list[iUid] = aNd
   }
   sNode.create.Unlock()
   return aNd
}

type tQueue struct {
   uid string
   conn net.Conn // client ref
   connDoor sync.Mutex // control access to conn
   ack chan string // forwards acks from client
   buf []string // elastic channel buffer
   in chan string // elastic channel input
   out chan string // elastic channel output
   hasConn int32 // in use by Link
}

func QueueLink(iUid string, iConn net.Conn) *tQueue {
   aNd := GetNode(iUid)
   if aNd.queue == nil {
      aNd.dir.Lock()
      if aNd.queue != nil {
         aNd.dir.Unlock()
         fmt.Printf(iUid+" newqueue attempt to recreate queue\n")
      } else {
         aNd.queue = new(tQueue)
         aQ := aNd.queue
         aQ.uid = iUid
         aQ.connDoor.Lock()
         aQ.ack = make(chan string, 10)
         aQ.in = make(chan string)
         aQ.out = make(chan string)
         var err error
         aQ.buf, err = sStore.GetDir(iUid)
         if err != nil { panic(err) }
         aNd.dir.Unlock()
         fmt.Printf(iUid+" newqueue create queue\n")
         go runElasticChan(aQ)
         go runQueue(aQ)
      }
   }
   if !atomic.CompareAndSwapInt32(&aNd.queue.hasConn, 0, 1) {
      return nil
   }
   iConn.Write(PackMsg(sMsgLogin, nil))
   aNd.queue.conn = iConn
   aNd.queue.connDoor.Unlock() // unblocks waitForConn
   return aNd.queue
}

func (o *tQueue) Unlink() {
   o.connDoor.Lock() // blocks waitForConn
   o.conn = nil
   o.hasConn = 0
}

func (o *tQueue) waitForConn() net.Conn {
   o.connDoor.Lock() // waits if o.conn nil
   aConn := o.conn
   o.connDoor.Unlock()
   return aConn
}

func runQueue(o *tQueue) {
   aMsgId := <-o.out
   aConn := o.waitForConn()
   for {
      aMsg, err := sStore.GetFile(o.uid, aMsgId)
      if err != nil { panic(err) }
      _, err = aConn.Write(aMsg)
      if err == nil {
         aTimeout := time.NewTimer(kQueueAckTimeout)
         select {
         case aAckId := <-o.ack:
            aTimeout.Stop()
            if aAckId != aMsgId {
               fmt.Printf("%s queue.runqueue got ack for %s, expected %s\n", aAckId, aMsgId)
               break
            }
            sStore.RmLink(o.uid, aMsgId)
            aMsgId = <-o.out
            aConn = o.waitForConn()
         case <-aTimeout.C:
            fmt.Printf("%s queue.runqueue timed out awaiting ack\n", o.uid)
         }
      } else if false { //todo transient
         time.Sleep(10 * time.Millisecond)
      } else {
         aConn = o.waitForConn()
         fmt.Printf("%s runqueue resumed\n", o.uid)
      }
   }
}

func runElasticChan(o *tQueue) {
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
   go runIdStore(o, &aWg)
   aWg.Wait()
}

func runIdStore(o *tStore, iWg *sync.WaitGroup) {
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

func (o *tStore) MakeId() string {
   aN := atomic.AddUint64(&o.nextId, 1)
   if aN % 1000 == 0 {
      o.idStore <- aN
   }
   return fmt.Sprintf("%016x", aN)
}

func (o *tStore) PutFile(iId string, iBuf []byte) error {
   aFd, err := os.OpenFile(o.temp+iId, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
   if err != nil { return err }
   defer aFd.Close()
   for aPos, aLen := 0,0; aPos < len(iBuf); aPos += aLen {
      aLen, err = aFd.Write(iBuf[aPos:])
      if err != nil && err != io.ErrShortWrite { return err }
   }
   err = aFd.Sync()
   return err
}

func (o *tStore) ZeroFile(iNode, iId string) error {
   aFd, err := os.OpenFile(o.nodeSub(iNode)+"/"+iId, os.O_WRONLY|os.O_TRUNC, 0600)
   if err != nil { return err }
   aFd.Close()
   return nil
}

func (o *tStore) PutLink(iSrc, iNode, iId string) error {
   aPath := o.nodeSub(iNode)
   err := os.MkdirAll(aPath, 0700)
   if err != nil { return err }
   err = os.Link(o.temp+iSrc, aPath+"/"+iId)
   return err
}

func (o *tStore) RmFile(iId string) error {
   return os.Remove(o.temp+iId)
}

func (o *tStore) RmLink(iNode, iId string) error {
   return os.Remove(o.nodeSub(iNode)+"/"+iId)
}

func (o *tStore) RmDir(iNode string) error {
   err := os.Remove(o.nodeSub(iNode))
   if os.IsNotExist(err) { return nil }
   return err
}

func (o *tStore) SyncDirs(iNode string) error {
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
   fSync(o.rootSub(iNode))
   if err != nil { return err }
   fSync(o.nodeSub(iNode))
   return err
}

func (o *tStore) GetFile(iNode, iId string) ([]byte, error) {
   return ioutil.ReadFile(o.nodeSub(iNode)+"/"+iId)
}

func (o *tStore) GetDir(iNode string) (ret []string, err error) {
   fmt.Printf("read dir %s\n", o.nodeSub(iNode))
   aFd, err := os.Open(o.nodeSub(iNode))
   if err != nil {
      if os.IsNotExist(err) { err = nil }
      return
   }
   ret, err = aFd.Readdirnames(0)
   sort.Slice(ret, func(i, j int) bool { return ret[i] < ret[j] })
   aFd.Close()
   return
}

func (o *tStore) rootSub(iNode string) string {
   return o.Root + strings.ToLower(iNode[:4])
}

func (o *tStore) nodeSub(iNode string) string {
   return o.rootSub(iNode) + "/" + strings.ToLower(iNode)
}

