package main

import (
   "fmt"
   "io/ioutil"
   "encoding/json"
   "os"
   //"qlib"
   "sync"
)

/*
func main() {
   aDb, err := NewUserDb("./userdb")
   if err != nil { panic(err) }
   aDb.user["u111111"] = &tUser{Nodes: map[string]int{"111111":1}}
   aDb.user["u222222"] = &tUser{Nodes: map[string]int{"222222":1}}
   aDb.user["u333333"] = &tUser{Nodes: map[string]int{"333333":1}}
   aDb.group["g1"] = &tGroup{Uid: map[string]tMember{
      "u111111": tMember{Alias: "111"},
      "u222222": tMember{Alias: "222"},
      "u333333": tMember{Alias: "333"},
   }}

   if false {
      aU, err := aDb.fetchUser("fetched", eFetchMake)
      if err != nil { panic(err) }
      aU.door.Lock()
      aU.Nodes["fi"] = 44
      aU.Aliases = append(aU.Aliases, tAlias{En:"fet"})
      aDb.putRecord(eTuser, "fetched", aU)
      aU.door.Unlock()
      delete(aDb.user, "fetched")
      aU, err = aDb.fetchUser("fetched", eFetchCheck)
      if err != nil { panic(err) }
      aU.door.RLock()
      fmt.Printf("node %v, alias %v\n", aU.Nodes["fi"], aU.Aliases[0].En)
      aU.door.RUnlock()
      return
   }

   qlib.UDb = aDb
   qlib.Init("qstore")

   fmt.Printf("Starting Test Pass\n")
   qlib.InitTestClient(2)
   for a := 0; true; a++ {
      aDawdle := a == 1
      qlib.NewLink(qlib.NewTestClient(aDawdle))
   }
}
*/

/* moved to qlib/testclient.go
const ( _=iota; eRegister; eAddNode; eLogin; eListEdit; ePost; ePing; eAck )

var sTestClientId chan int

type tTestClient struct {
   id, to int // who i am, who i send to
   count int // msg number
   deferLogin bool // test login timeout feature
   ack chan int // writer tells reader to issue ack to qlib
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
      ack: make(chan int,10),
   }
}

func (o *tTestClient) Read(buf []byte) (int, error) {
   if o.count % 10 == 9 {
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
      return 0, &net.OpError{Op:"read", Err:&tTimeoutError{}}
   }

   aMsg := qlib.PackMsg(aHead, []byte(aData))
   fmt.Printf("%d testclient.read %s\n", o.id, string(aMsg))
   return copy(buf, aMsg), nil
}

func (o *tTestClient) Write(buf []byte) (int, error) {
   if o.closed {
      fmt.Printf("%d testclient.write was closed\n", o.id)
      return 0, &net.OpError{Op:"write", Err:tTestClientError("closed")}
   }

   aTmr := time.NewTimer(2 * time.Second)

   select {
   case o.ack <- 1:
      aTmr.Stop()
   case <-aTmr.C:
      fmt.Printf("%d testclient.write timed out on ack\n", o.id)
      return 0, &net.OpError{Op:"write", Err:tTestClientError("noack")}
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
   time.AfterFunc(10*time.Millisecond, func(){ sTestClientId <- o.id })
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

type tTestClientError string
func (o tTestClientError) Error() string { return string(o) }

type tMsg map[string]interface{}
*/


//: these are instructions/guidance comments
//: you'll implement the public api to add/edit userdb records
//: for all ops, you look up a record in cache,
//:   and if not there call getRecord and cache the result
//:   lookups are done with aObj := o.user[Uid] (or o.alias, o.group)
//: for add/edit ops, you then modify the cache object, then call putRecord
//: locking
//:   cache read ops are done inside o.xyzDoor.RLock/RUnlock()
//:   cache add/delete ops are done inside o.xyzDoor.Lock/Unlock()
//:   tUser and tGroup object updates are done inside aObj.door.Lock/Unlock()
//: records are stored as files in subdirectories of o.root: user, alias, group
//:   user/* & group/* files are json format
//:   alias/* files are symlinks to Uid

type tUserDb struct {
   root string // top-level directory
   temp string // temp subdirectory; write files here first

   // cache records here
   userDoor sync.RWMutex
   user map[string]*tUser

   aliasDoor sync.RWMutex
   alias map[string]string // value is Uid

   groupDoor sync.RWMutex
   group map[string]*tGroup
}

type tUser struct {
   door sync.RWMutex
   Nodes map[string]tNode // value is NodeRef
   NonDefunctNodesCount int
   Aliases []tAlias // public names for the user
}

type tNode struct {
  Defunct bool
  Qid string
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
   Alias string // invited/joined by this alias
   Joined bool // use a date here?
}

type tUserDbErr string
func (o tUserDbErr) Error() string { return string(o) }

type tType string
const (
   eTuser  tType = "user"
   eTalias tType = "alias"
   eTgroup tType = "group"
)

//: add a crash recovery pass on startup
//: examine temp dir
//:   complete any pending transactions
//: in transaction
//:   sync temp dir instead of data dir
//:   remove temp file in commitDir
//:   drop .tmp files

func NewUserDb(iPath string) (*tUserDb, error) {
   for _, a := range [...]tType{ "temp", eTuser, eTalias, eTgroup } {
      err := os.MkdirAll(iPath + "/" + string(a), 0700)
      if err != nil { return nil, err }
   }

   aDb := new(tUserDb)
   aDb.root = iPath+"/"
   aDb.temp = aDb.root + "temp"
   aDb.user = make(map[string]*tUser)
   aDb.alias = make(map[string]string)
   aDb.group = make(map[string]*tGroup)

   return aDb, nil
}

func (o *tUserDb) Test() error {
   //: exercise the api, print diagnostics
   //: invoke from main() before tTestClient loop; stop program if tests fail
   fmt.Println("Testing AddUser")
   aAddedUserQid, err := o.AddUser("User1","Node1")
   fmt.Println(aAddedUserQid, err)
   aAddedUserQid, err = o.AddUser("User1", "Node2") // iUid that already exists
   fmt.Println(aAddedUserQid, err)
   fmt.Println("AddUser tests complete")

   return nil
}

//: below is the public api
//: if same parameters are retried after success, ie data already exists,
//:   function should do nothing but return success

func (o *tUserDb) AddUser(iUid, iNewNode string) (aQid string, err error) {
   //: add user
   //: iUid not in o.user, or already has iNewNode
   aUser,err := o.fetchUser(iUid, eFetchMake)

   aQid = iNewNode //todo generate Qid properly

   if err != nil {
      return "", nil
   }

   aUser.door.Lock()
   defer aUser.door.Unlock()

   if len(aUser.Nodes) != 0 {
      if aUser.Nodes[iNewNode].Qid == aQid {
         return aQid, nil
      }
      return "", tUserDbErr("err msg") //iNewNode already exists but it's Qid does not match
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Qid: aQid}
   aUser.NonDefunctNodesCount++

   o.putRecord(eTuser, iUid, aUser)
   return aQid, nil
}

const kUserNodeMax = 100
func (o *tUserDb) AddNode(iUid, iNode, iNewNode string) (aQid string, err error) {
   //: add node
   //: iUid has iNode
   //: iUid may already have iNewNode
}

func (o *tUserDb) DropNode(iUid, iNode string) (aQid string, err error) {
   //: mark iNode defunct
   //: iUid has iNode

   //: Check if iUid has iNode.
   //: Check if iNode is already defunct. If it is, return success.
   //: Check if there are 2 non-defunct nodes in iUid. If no, error. If yes, defunct that node.

   //: Requires change of tUserDb. Nodes will be map[string]struct, struct contains string Qid and bool defunct
   //: Define new type- tNode

   //: When assigning to Nodes map, make a compound literal tNode that changes either the string Qid or the bool defunct
   //: (will have copy of one value and one changed value)

   //: Error- iUid does not have iNode
   //: Error- you only have one node left

   /* ACTION PLAN
    * 1. Check if iUid has iNode. If not, return error.
    * 2. Check if iNode is already defunct. If it is:
         aQid = iNode
         return aQid
    * 3. Lock userDoor, make iNode in Nodes map defunct, decrement nonDefunctNodesCount
         o.userDoor.Lock()
         o.user.Nodes[iNode] = tNode{defunct: true, Qid: iNode}
         o.user.nonDefunctNodesCount--
         o.userDoor.Unlock()
    * 4. aQid = iNode
         return aQid
    */

   aUser, err := o.fetchUser(iUid, eFetchCheck) // need to check if user is valid, if not, return error
   if err != nil {
     return "", err
   }
   if aUser == nil {
      return "", tUserDbErr("err msg")
   }
   aUser.door.Lock()
   defer aUser.door.Unlock()

   aQid = iNode
   if aUser.Nodes[iNode].Qid != aQid {
      return "", tUserDbErr("err msg")
   }
   if aUser.Nodes[iNode].Defunct {
      return aQid, nil
   }
   if aUser.NonDefunctNodesCount <= 1 {
      return "", tUserDbErr("err msg")
   }

   aUser.Nodes[iNode] = tNode{Defunct: true, Qid: iNode}
   aUser.NonDefunctNodesCount--
   return aQid, nil

   o.putRecord(eTuser, iUid, aUser)
   return "", nil
}

func (o *tUserDb) AddAlias(iUid, iNode, iNat, iEn string) error {
   //: add aliases to iUid and o.alias
   //: iUid has iNode
   //: iNat != iEn, iNat or iEn != ""
   return nil
}

func (o *tUserDb) DropAlias(iUid, iNode, iAlias string) error {
   //: mark alias defunct in o.alias
   //: iUid has iNode
   //: iAlias for iUid
   return nil
}

//func (o *tUserDb) DropUser(iUid string) error { return nil }

func (o *tUserDb) Verify(iUid, iNode string) (aQid string, err error) {
   //: return Qid of node
   //: iUid has iNode
   // trivial implementation for qlib testing
   if o.user[iUid] != nil && !o.user[iUid].Nodes[iNode].Defunct {
      return "q"+iNode, nil
   }
   return "", tUserDbErr("no such user/node")
}

func (o *tUserDb) GetNodes(iUid string) (aQids []string, err error) {
   //: return Qids for iUid
   // trivial implementation for qlib testing
   for aN,_ := range o.user[iUid].Nodes {
      aQids = append(aQids, aN)
   }
   return aQids, nil
}

func (o *tUserDb) Lookup(iAlias string) (aUid string, err error) {
   //: return uid for iAlias
   // trivial implementation for qlib testing
   switch iAlias {
   case "a1": return "u111111", nil
   case "a2": return "u222222", nil
   }
   return "", tUserDbErr(iAlias+" not known")
}

func (o *tUserDb) GroupInvite(iGid, iAlias, iByAlias, iByUid string) error {
   //: add member to group, possibly create group
   //: iAlias exists
   //: iGid exists, iByUid in group, iByAlias ignored
   //: iGid !exists, make iGid and add iByUid with iByAlias
   //: iByAlias for iByUid
   return nil
}

func (o *tUserDb) GroupJoin(iGid, iUid, iNewAlias string) error {
   //: set joined status for member
   //: iUid in group
   //: iNewAlias optional for iUid
   return nil
}

func (o *tUserDb) GroupAlias(iGid, iUid, iNewAlias string) error {
   //: update member alias
   //: iUid in group
   //: iNewAlias for iUid
   return nil
}

func (o *tUserDb) GroupDrop(iGid, iUid, iByUid string) error {
   //: change member status of member with iUid
   //: iUid in group, iByUid same or in group
   //: iUid == iByUid, status=invited
   //: iUid != iByUid, if iUid status==joined, status=barred else delete member
   return nil
}

func (o *tUserDb) GroupGetUsers(iGid, iByUid string) (aUids []string, err error) {
   //: return uids in iGid
   //: iByUid is member
   for a,_ := range o.group["g1"].Uid {
      aUids = append(aUids, a)
   }
   return aUids, nil
}

type tFetch bool
const eFetchCheck, eFetchMake tFetch = false, true

// retrieve user from cache or disk
func (o *tUserDb) fetchUser(iUid string, iMake tFetch) (*tUser, error) {
   o.userDoor.RLock() // read-lock user map
   aUser := o.user[iUid] // lookup user in map
   o.userDoor.RUnlock()

   if aUser == nil { // user not in cache
      aObj, err := o.getRecord(eTuser, iUid) // lookup user on disk
      if err != nil { return nil, err }
      aUser = aObj.(*tUser) // "type assertion" extracts *tUser from interface{}

      if aUser.Nodes == nil { // user not on disk
         if !iMake {
            return nil, nil
         }
         aUser.Nodes = make(map[string]tNode) // initialize user
      }

      o.userDoor.Lock() // write-lock user map
      if aTemp := o.user[iUid]; aTemp != nil { // recheck the map
         aUser = aTemp
      } else {
         o.user[iUid] = aUser // add user to map
      }
      o.userDoor.Unlock()
   }
   return aUser, nil // do .door.[R]Lock() on return value before use
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

   switch iType {
   default:
      panic("getRecord: unexpected type "+iType)
   case eTalias:
      aLn, err := os.Readlink(aPath)
      if err != nil {
         if os.IsNotExist(err) { return nil, nil }
         return nil, err
      }
      return &aLn, nil
   case eTuser:  aObj = &tUser{}
   case eTgroup: aObj = &tGroup{}
   }

   aBuf, err := ioutil.ReadFile(aPath)
   if err != nil {
      if os.IsNotExist(err) { return aObj, nil }
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

   switch iType {
   default:
      panic("putRecord: unexpected type "+iType)
   case eTalias:
      err = os.Symlink(iObj.(string), aPath + ".tmp")
      if err != nil { return err }
      return o.commitDir(iType, aPath)
   case eTuser, eTgroup:
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
