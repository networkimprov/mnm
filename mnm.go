package main

import (
   "fmt"
   "io/ioutil"
   "encoding/json"
   "net"
   "os"
   "qlib"
   "sync"
   "time"
)

var sId chan int // recycles client ids back to main()
var sTimeout error = &tTimeoutError{}

func main() {
   aDb, err := NewUserDb("./userdb")
   if err != nil { panic(err) }

   qlib.UDb = aDb
   qlib.Init("qstore")
   sId = make(chan int, 10)

   fmt.Printf("Starting Test Pass\n")
   sId <- 111111
   sId <- 222222
   for a := 0; true; a++ {
      aDawdle := a == 1
      qlib.NewLink(NewTc(<-sId, aDawdle))
   }
}

const ( _=iota; eRegister; eAddNode; eLogin; eListEdit; ePost; ePing; eAck )


type tTestClient struct {
   id, to int // who i am, who i send to
   count int // msg number
   deferLogin bool // test login timeout feature
   ack chan int // writer tells reader to issue ack to qlib
   closed bool // when about to shut down
   readDeadline time.Time // set by qlib
}

func NewTc(i int, iDawdle bool) *tTestClient {
   return &tTestClient{
      id: i, to: i+111111,
      deferLogin: iDawdle,
      ack: make(chan int,10),
   }
}

func (o *tTestClient) Read(buf []byte) (int, error) {
   if o.count % 10 == 9 {
      return 0, &net.OpError{Op:"log out"}
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
   case <-o.ack:
      aHead = tMsg{"Op":eAck, "Id":"n", "Type":"n"}
   case <-aTmr.C:
      o.count++
      if o.deferLogin {
         aHead = tMsg{}
      } else if o.count == 1 {
         aHead = tMsg{"Op":eLogin, "Uid":"u"+fmt.Sprint(o.id), "NodeId":fmt.Sprint(o.id)}
      } else {
         aHead = tMsg{"Op":ePost, "Id":"n", "For":[]string{"u"+fmt.Sprint(o.to)}}
         aData = fmt.Sprintf(" |msg %d|", o.count)
      }
   case <-aDlC:
      return 0, &net.OpError{Op:"timeout",Err:sTimeout}
   }

   aMsg := qlib.PackMsg(aHead, []byte(aData))
   fmt.Printf("%d testclient.read %s\n", o.id, string(aMsg))
   return copy(buf, aMsg), nil
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

func (o *tTestClient) SetReadDeadline(i time.Time) error {
   o.readDeadline = i
   return nil
}

func (o *tTestClient) Close() error {
   o.closed = true;
   time.AfterFunc(10*time.Millisecond, func(){ sId <- o.id })
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

type tMsg map[string]interface{}


//: these are instructions/guidance comments
//: you'll implement the public api to add/edit userdb records
//: for all ops, you look up a record in cache,
//:   and if not there call getRecord and cache the result
//:   lookups are done with aObj := o.Uid[iUid] (or o.Alias, o.Group)
//: for add/edit ops, you then modify the cache object, then call putRecord
//: locking
//:   cache read ops are done inside o.xyzDoor.RLock/RUnlock()
//:   cache add/delete ops are done inside o.xyzDoor.Lock/Unlock()
//:   Uid and Group object updates are done inside aObj.door.Lock/Unlock()
//: records are stored as files in subdirectories of o.root: uid, alias, group
//:   uid/* & group/* files are json format
//:   alias/* files are symlinks to Uid

type tUserDb struct {
   root string // top-level directory
   temp string // temp subdirectory; write files here first
   uidDoor, alsDoor, lstDoor sync.RWMutex // protect cache during update

   // cache records here
   Uid map[string]*tUser
   Alias map[string]string // value is Uid
   Group map[string]*tGroup
}

type tUser struct {
   door sync.RWMutex
   Nodes map[string]int // value is NodeRef
   Aliases []tAlias // public names for the user
}

type tAlias struct {
   En string // in english
   Nat string // in whatever language
}

type tGroup struct {
   door sync.RWMutex
   Uid map[string]tMember
}

type tMember struct {
   Alias string // invited/joined by this Alias
   Joined bool // use a date here?
}

//type tUserDbErr string
//func (o tUserDbErr) Error() string { return string(o) }

type tType string
const (
   eTuid   tType = "uid"
   eTalias tType = "alias"
   eTgroup  tType = "group"
)

//: add a crash recovery pass on startup
//: examine temp dir
//:   complete any pending transactions
//: in transaction
//:   sync temp dir instead of data dir
//:   remove temp file in commitDir
//:   drop .tmp files

func NewUserDb(iPath string) (*tUserDb, error) {
   for _, a := range [...]tType{ "temp", eTuid, eTalias, eTgroup } {
      err := os.MkdirAll(iPath + "/" + string(a), 0700)
      if err != nil { return nil, err }
   }

   aDb := new(tUserDb)
   aDb.root = iPath+"/"
   aDb.temp = aDb.root + "temp"
   aDb.Uid = make(map[string]*tUser)
   aDb.Alias = make(map[string]string)
   aDb.Group = make(map[string]*tGroup)

   return aDb, nil
}

func (o *tUserDb) Test() error {
   //: exercise the api, print diagnostics
   //: invoke from main() before tTestClient loop; stop program if tests fail
   return nil
}

//: below is the public api

func (o *tUserDb) AddUser(iUid, iNewNode string, iAliases []string) (aAliases []string, err error) {
   //: add user if iUid not in db
   return []string{}, nil
}

func (o *tUserDb) SetAliases(iUid, iNode string, iAliases []string) (aAliases []string, err error) {
   //: replace aliases if iUid in db and has iNode, and iAliases elements are unique
   return []string{}, nil
}

func (o *tUserDb) AddNode(iUid, iNode, iNewNode string) (aNodeRef int, err error) {
   //: add iNewNode if iUid in db and has iNode
   return 0, nil
}

func (o *tUserDb) DropNode(iUid, iNode string) error {
   //: delete iNode if iUid in db and has iNode
   return nil
}

//func (o *tUserDb) DropUser(iUid string) error {
//   return nil
//}

func (o *tUserDb) Verify(iUid, iNode string) (aNodeRef int, err error) {
   //: return noderef if iUid in db and has iNode
   // trivial implementation for qlib testing
   o.Uid[iUid] = tUser{Nodes: map[string]int{iNode:0}}
   return 0, nil
}

func (o *tUserDb) GetNodes(iUid string) (aNodes []string, err error) {
   //: return noderefs if iUid in db
   // trivial implementation for qlib testing
   for aN,_ := range o.Uid[iUid].Nodes {
      aNodes = append(aNodes, aN)
   }
   return aNodes, nil
}

func (o *tUserDb) Lookup(iAlias string) (aUid string, err error) {
   //: return uid if iAlias in db
   return "", nil
}

func (o *tUserDb) ListInvite(iList, iBy, iAlias string) error {
   //: if iAlias in db, and iBy in db & iList (or make iList and add iBy), list iAlias
   return nil
}

func (o *tUserDb) ListJoin(iList, iAlias, iUid string) (aAlias string, err error) {
   //: return listed alias if iList in db and iUid in iList, list iAlias if != ""
   return "", nil
}

func (o *tUserDb) ListDrop(iList, iBy, iUid string) error {
   //: remove iUid from iList if iBy in iList
   return nil
}

func (o *tUserDb) ListLookup(iList, iBy string) (aUids []string, err error) {
   //: return uids if iBy in list
   return []string{}, nil
}

// pull a file into a cache object
func (o *tUserDb) getRecord(iType tType, iId string) (interface{}, error) {
   var err error
   var aObj interface{}
   aPath := o.root + string(iType) + "/" + iId

   // in case putRecord was interrupted
   err = os.Link(aPath + ".tmp", aPath)
   if err != nil {
      if !os.IsExist(err) && !os.IsNotExist(err) { return nil, err }
   } else {
      fmt.Println("getRecord: finished transaction for "+aPath)
   }

   switch (iType) {
   default:
      panic("getRecord: unexpected type "+iType)
   case "alias":
      aLn, err := os.Readlink(aPath)
      if err != nil {
         if os.IsNotExist(err) { return nil, nil }
         return nil, err
      }
      return &aLn, nil
   case "uid":  aObj = &tUser{}
   case "group": aObj = &tGroup{}
   }

   aBuf, err := ioutil.ReadFile(aPath)
   if err != nil {
      if os.IsNotExist(err) { return nil, nil }
      return nil, err
   }

   err = json.Unmarshal(aBuf, aObj)
   return aObj, err
}

// save cache object to disk. getRecord must be called before this
func (o *tUserDb) putRecord(iType tType, iId string, iObj interface{}) error {
   var err error
   aPath := o.root + string(iType) + "/" + iId
   aTemp := o.temp + string(iType) + "_" + iId

   err = os.Remove(aPath + ".tmp")
   if err == nil {
      fmt.Println("putRecord: removed residual .tmp file for "+aPath)
   }

   switch (iType) {
   default:
      panic("putRecord: unexpected type "+iType)
   case "alias":
      err = os.Symlink(iObj.(string), aPath + ".tmp")
      if err != nil { return err }
      return o.commitDir(iType, aPath)
   case "uid", "group":
   }

   aBuf, err := json.Marshal(iObj)
   if err != nil { return err }

   aFd, err := os.OpenFile(aTemp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
   if err != nil { return err }
   defer aFd.Close()

   for aPos,aLen := 0,0; aPos < len(aBuf); aPos += aLen {
      aLen, err = aFd.Write(aBuf[aPos:])
      if err != nil { return err }
   }

   err = aFd.Sync()
   if err != nil { return err }

   err = os.Link(aTemp, aPath + ".tmp")
   if err != nil { return err }
   err = os.Remove(aTemp)
   if err != nil { return err }

   return o.commitDir(iType, aPath)
}

// sync the directory and set the filename
func (o *tUserDb) commitDir(iType tType, iPath string) error {
   aFd, err := os.Open(o.root + string(iType))
   if err != nil { return err }
   defer aFd.Close()
   err = aFd.Sync()
   if err != nil { return err }

   err = os.Remove(iPath)
   if err != nil && !os.IsNotExist(err) { return err }
   err = os.Rename(iPath + ".tmp", iPath)
   return err
}
