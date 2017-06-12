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
const kQueueIdleMax time.Duration = 28 * time.Hour
const kStoreIdIncr = 1000

var sNode tNodes
var sStore tStore

func init() {
   sNode.list = make(tNodeMap)
}


type Link struct { // network client msg handler
   conn net.Conn // link to client
   connSet chan<- net.Conn // updates tQueue
   ack chan<- string // forwards client acks to queue thread
   uid string
}

func NewLink(iConn net.Conn) *Link {
   aL := &Link{conn:iConn}
   go runLink(aL)
   return aL
}

func runLink(o *Link) {
   o.conn.SetReadDeadline(time.Now().Add(kLoginTimeout))
   aBuf := make([]byte, 64)
   for {
      aLen, err := o.conn.Read(aBuf)
      if err != nil {
         //todo if recoverable continue
         if err.(net.Error).Timeout() {
            o.conn.Write([]byte("login timeout"))
         }
         if o.connSet != nil {
            o.connSet <- nil
         }
         o.conn.Close()
         break
      }
      aHeadEnd, err := strconv.ParseUint(string(aBuf[:4]), 16, 0)
      if err != nil { panic(err) }
      aHeadEnd += 4
      var aMsg tHeader
      err = json.Unmarshal(aBuf[4:aHeadEnd], &aMsg)
      if err != nil { panic(err) }
      var aData []byte
      if aLen > int(aHeadEnd) {
         aData = aBuf[aHeadEnd:]
      }
      o.HandleMsg(&aMsg, aData)
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
   For []string
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

func (o *Link) HandleMsg(iMsg *tHeader, iData []byte) {
   switch(iMsg.Op) {
   case eLogin:
      aQ := NewQueue(iMsg.Uid)
      o.conn.SetReadDeadline(time.Time{})
      o.uid = iMsg.Uid
      o.ack = aQ.ack
      o.connSet = aQ.connIn
      o.connSet <- o.conn
      fmt.Printf("%s link.handlemsg login user %s\n", o.uid, aQ.uid)
   case ePost:
      aId := sStore.MakeId()
      sStore.PutFile(aId, iData)
      aNd := GetNode(iMsg.To)
      aNd.dir.RLock()
      sStore.PutLink(aId, iMsg.To, aId)
      sStore.SyncDirs(iMsg.To)
      if aNd.queue != nil {
         aNd.queue.in <- aId
      }
      aNd.dir.RUnlock()
      sStore.RmFile(aId)
   case eAck:
      aTmr := time.NewTimer(2 * time.Second)
      select {
      case o.ack <- "bing":
         aTmr.Stop()
      case <-aTmr.C:
         fmt.Printf("%s link.handlemsg timed out waiting on ack\n", o.uid)
      }
   default:
      fmt.Printf("unknown msg type %s\n", iMsg.Type)
   }
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
   connIn chan net.Conn // Link updates client ref here
   connOut chan net.Conn // runQueue receives client ref here
   ack chan string // forwards acks from client
   buf []string // elastic channel buffer
   in chan string // elastic channel input
   out chan string // elastic channel output
}

func NewQueue(iUid string) *tQueue {
   aNd := GetNode(iUid)
   if aNd.queue == nil {
      aNd.dir.Lock()
      if aNd.queue != nil { panic("attempt to recreate queue for node "+iUid) }
      fmt.Printf("%s newqueue make queue\n", iUid)
      aNd.queue = new(tQueue)
      aQ := aNd.queue
      aQ.uid = iUid
      aQ.connIn = make(chan net.Conn)
      aQ.connOut = make(chan net.Conn)
      aQ.ack = make(chan string, 10)
      aQ.in = make(chan string)
      aQ.out = make(chan string)
      var err error
      aQ.buf, err = sStore.GetDir(iUid)
      if err != nil { panic(err) }
      aNd.dir.Unlock()
      go runConnChan(aQ)
      go runElasticChan(aQ)
      go runQueue(aQ)
   }
   return aNd.queue
}

func runQueue(o *tQueue) {
   aId := <-o.out
   aConn := <-o.connOut
   for {
      aMsg, err := sStore.GetFile(o.uid, aId)
      if err != nil { panic(err) }
      _, err = aConn.Write(aMsg)
      if err == nil {
         aTimeout := time.NewTimer(5 * time.Second)
         select {
         case <-o.ack:
            aTimeout.Stop()
            sStore.RmLink(o.uid, aId)
            aId = <-o.out
            aConn = <-o.connOut
         case <-aTimeout.C:
            fmt.Printf("%s queue.runqueue timed out awaiting ack\n", o.uid)
         }
      } else if false { //todo transient
         time.Sleep(10 * time.Millisecond)
      } else {
         aConn = <-o.connOut
         fmt.Printf("%s runqueue resumed\n", o.uid)
      }
   }
}

func runConnChan(o *tQueue) {
   var aConn net.Conn
   var ok bool
   for {
      for aConn == nil {
         aConn, ok = <-o.connIn
         if !ok { goto closed }
      }
      select {
         case aConn, ok = <-o.connIn:
            if !ok { goto closed }
         case o.connOut <- aConn:
      }
   }
closed:
   close(o.connOut)
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

