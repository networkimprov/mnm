package main

import (
   "fmt"
   "io/ioutil"
   "encoding/json"
   "os"
   "sync"
)

const kUserNodeMax = 100
const kAliasDefunctUid = "*defunct"


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
   sync.RWMutex
   Nodes map[string]tNode
   NonDefunctNodesCount int
   Aliases []tAlias // public names for the user
}

type tNode struct {
  Defunct bool
  Qid string
}

type tAlias struct {
   En string // in english
   EnDefunct bool
   Nat string // in whatever language
   NatDefunct bool
}

type tGroup struct {
   sync.RWMutex
   Uid map[string]tMember
}

type tMember struct {
   Alias string // invited/joined by this alias
   Joined bool // use a date here?
}

type tUdbError struct {
   msg string
   id int
}
func (o *tUdbError) Error() string { return string(o.msg) }

const (
   _=iota;
   eErrArgument;
   eErrMissingNode;
   eErrUserInvalid; eErrNodeInvalid; eErrMaxNodes; eErrLastNode;
   eErrUnknownAlias; eErrAliasTaken; eErrAliasInvalid;
)

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

func TestUserDb(iPath string) {
   //: exercise the api, print diagnostics
   //: invoke from main() before tTestClient loop; stop program if tests fail
   _ = os.RemoveAll(iPath)
   aDb, err := NewUserDb(iPath)
   if err != nil { panic(err) }
   defer os.RemoveAll(iPath) // comment out for debugging

   aOk := true
   fReport := func(cMsg string) {
      aOk = false
      if err != nil {
         fmt.Fprintf(os.Stderr, "%s: %s\n", cMsg, err.Error())
      } else {
         fmt.Fprintf(os.Stderr, cMsg + "\n")
      }
   }

   var aUid1, aUid2, aNode1, aNode2 string
   aNat := "アリアス"

   // ADDUSER
   aUid1 = "AddUserUid1"
   aNode1 = "AddUserN1"
   _, err = aDb.AddUser(aUid1, aNode1)
   if err != nil || aDb.user[aUid1].Nodes[aNode1].Qid != aNode1 {
      fReport("add case failed")
   }
   _, err = aDb.AddUser(aUid1, aNode1)
   if err != nil || aDb.user[aUid1].Nodes[aNode1].Qid != aNode1 {
      fReport("re-add case failed")
   }
   _, err = aDb.AddUser(aUid1, "AddUserN0")
   if err == nil || err.(*tUdbError).id != eErrMissingNode {
      fReport("add existing case succeeded: AddUser")
   }

   // ADDNODE
   aUid1, aUid2 = "AddUserUid1", "AddNodeUid2"
   aNode1, aNode2 = "AddUserN1", "AddNodeN2"
   _, err = aDb.AddNode(aUid1, aNode1, aNode2)
   if err != nil || aDb.user[aUid1].Nodes[aNode2].Qid != aNode2 {
      fReport("add case failed")
   }
   _, err = aDb.AddNode(aUid1, aNode1, aNode2)
   if err != nil || aDb.user[aUid1].Nodes[aNode2].Qid != aNode2 {
      fReport("re-add case failed")
   }
   _, err = aDb.AddNode(aUid1, aNode1, aNode1)
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("iNode==iNewNode case succeeded: AddNode")
   }
   _, err = aDb.AddNode(aUid1, "AddNodeN0", aNode2)
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("invalid node case succeeded: AddNode")
   }
   _, err = aDb.AddNode("AddNodeUid0", aNode1, aNode2)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: AddNode")
   }
   aDb.AddUser(aUid2, aNode2)
   for a := 1; a < 100; a++ {
      _, err = aDb.AddNode(aUid2, aNode2, "AddNodeN0"+fmt.Sprint(a))
      if err != nil {
         fReport("add 100 case failed")
         break
      }
   }
   _, err = aDb.AddNode(aUid2, aNode2, "AddNodeN100")
   if err == nil || err.(*tUdbError).id != eErrMaxNodes {
      fReport("add >100 case succeeded: AddNode")
   }

   // DROPNODE
   aUid1, aUid2 = "AddUserUid1", "DropNodeUid2"
   aNode1, aNode2 = "AddNodeN2", "DropNodeN2"
   _, err = aDb.DropNode(aUid1, aNode1)
   if err != nil || ! aDb.user[aUid1].Nodes[aNode1].Defunct {
      fReport("drop case failed")
   }
   _, err = aDb.DropNode(aUid1, aNode1)
   if err != nil || ! aDb.user[aUid1].Nodes[aNode1].Defunct {
      fReport("re-drop case failed")
   }
   _, err = aDb.DropNode(aUid1, "DropNodeN0")
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("invalid node case succeeded: DropNode")
   }
   _, err = aDb.DropNode("DropNodeUid0", "AddUserN1")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: DropNode")
   }
   aDb.AddUser(aUid2, aNode2)
   _, err = aDb.DropNode(aUid2, aNode2)
   if err == nil || err.(*tUdbError).id != eErrLastNode {
      fReport("last node case succeeded: DropNode")
   }

   // ADDALIAS
   aUid1, aUid2 = "AddUserUid1", "AddAliasUid2"
   aNode1, aNode2 = "AddUserN1", "AddAliasN2"
   err = aDb.AddAlias(aUid1, aNode1, aNat, "AddAliasA1")
   if err != nil || aDb.alias[aNat] != aUid1 || aDb.alias["AddAliasA1"] != aUid1 {
      fReport("add both case failed")
   }
   err = aDb.AddAlias(aUid1, aNode1, aNat, "AddAliasA1")
   if err != nil || aDb.alias[aNat] != aUid1 || aDb.alias["AddAliasA1"] != aUid1 {
      fReport("re-add both case failed")
   }
   err = aDb.AddAlias(aUid1, aNode1, "", "AddAliasA2")
   if err != nil || aDb.alias["AddAliasA2"] != aUid1 {
      fReport("add en case failed")
   }
   err = aDb.AddAlias(aUid1, aNode1, aNat+"2", "")
   if err != nil || aDb.alias[aNat+"2"] != aUid1 {
      fReport("add nat case failed")
   }
   err = aDb.AddAlias(aUid1, aNode1, aNat+"2", "AddAliasA3")
   if err != nil || aDb.alias[aNat+"2"] != aUid1 || aDb.alias["AddAliasA3"] != aUid1 {
      fReport("re-add nat case failed")
   }
   err = aDb.AddAlias(aUid1, aNode1, aNat, aNat)
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("iNat==iEn case succeeded: AddAlias")
   }
   err = aDb.AddAlias(aUid1, "AddAliasN0", aNat, "")
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("invalid node case succeeded: AddAlias")
   }
   err = aDb.AddAlias("AddAliasUid0", aNode1, aNat, "")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: AddAlias")
   }
   aDb.AddUser(aUid2, aNode2)
   err = aDb.AddAlias(aUid2, aNode2, aNat, "")
   if err == nil || err.(*tUdbError).id != eErrAliasTaken {
      fReport("already taken case succeeded: AddAlias")
   }

   // DROPALIAS
   aUid1, aUid2 = "AddUserUid1", "AddAliasUid2"
   aNode1, aNode2 = "AddUserN1", "AddAliasN2"
   err = aDb.DropAlias(aUid1, aNode1, "AddAliasA1")
   if err != nil || aDb.alias["AddAliasA1"] != kAliasDefunctUid {
      fReport("drop en case failed")
   }
   err = aDb.DropAlias(aUid1, aNode1, "AddAliasA1")
   if err != nil || aDb.alias["AddAliasA1"] != kAliasDefunctUid {
      fReport("re-drop en case failed")
   }
   err = aDb.DropAlias(aUid1, aNode1, aNat)
   if err != nil || aDb.alias[aNat] != kAliasDefunctUid {
      fReport("drop nat case failed")
   }
   err = aDb.DropAlias(aUid1, "DropAliasN1", aNat)
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("invalid node case succeeded: DropAlias")
   }
   err = aDb.DropAlias("DropAliasUid0", aNode1, aNat)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: DropAlias")
   }
   err = aDb.DropAlias(aUid2, aNode2, "AddAliasA2")
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid alias case succeeded: DropAlias")
   }

   // VERIFY
   aUid1 = "AddUserUid1"
   aNode1 = "AddUserN1"
   _, err = aDb.Verify(aUid1, aNode1)
   if err != nil {
      fReport("verify case failed")
   }
   _, err = aDb.Verify(aUid1, "VerifyN0")
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("invalid node case succeeded: Verify")
   }
   _, err = aDb.Verify("VerifyUid0", aNode1)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: Verify")
   }

   // OPEN/CLOSENODES
   aUid1 = "AddUserUid1"
   var aNodes []string
   aNodes, err = aDb.OpenNodes(aUid1)
   if err != nil || len(aNodes) != 1 {
      fReport("opennodes case failed")
   }
   err = aDb.CloseNodes(aUid1)
   if err != nil {
      fReport("closenodes case failed")
   }
   _, err = aDb.OpenNodes("OpenNodesUid0")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
     fReport("invalid user case succeeded: OpenNodes")
   }
   err = aDb.CloseNodes("CloseNodesUid0")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
     fReport("invalid user case succeeded: CloseNodes")
   }

   if aOk {
      fmt.Println("UserDb tests passed")
   }
}

//: below is the public api
//: if same parameters are retried after success, ie data already exists,
//:   function should do nothing but return success

func (o *tUserDb) AddUser(iUid, iNewNode string) (aQid string, err error) {
   //: add user
   //: iUid not in o.user, or already has iNewNode
   aUser, err := o.fetchUser(iUid, eFetchMake)
   if err != nil { panic(err) }

   aUser.Lock()
   defer aUser.Unlock()

   aQid = iNewNode //todo generate Qid properly

   if len(aUser.Nodes) != 0 {
      if aUser.Nodes[iNewNode].Qid != aQid {
         return "", &tUdbError{id: eErrMissingNode,
                       msg: fmt.Sprintf("AddUser: Uid %s found, Node %s missing", iUid, iNewNode)}
      }
      return aQid, nil
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Qid: aQid}
   aUser.NonDefunctNodesCount++

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return aQid, nil
}

func (o *tUserDb) AddNode(iUid, iNode, iNewNode string) (aQid string, err error) {
   //: add node
   //: iUid has iNode
   //: iUid may already have iNewNode
   if iNode == iNewNode {
      return "", &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddNode: iNode & iNewNode both %s", iNode)}
   }

   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("AddNode: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   aNodeQid := iNode //todo generate properly
   aNewNodeQid := iNewNode
   if aUser.Nodes[iNode].Qid != aNodeQid {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("AddNode: iNode %s invalid", iNode)}
   }
   if aUser.Nodes[iNewNode].Qid == aNewNodeQid {
      return aNewNodeQid, nil
   }
   if aUser.NonDefunctNodesCount == kUserNodeMax {
      return "", &tUdbError{id: eErrMaxNodes, msg: fmt.Sprintf("AddNode: Exceeds %d nodes", kUserNodeMax)}
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Qid: aNewNodeQid}
   aUser.NonDefunctNodesCount++

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return aNewNodeQid, nil
}

func (o *tUserDb) DropNode(iUid, iNode string) (aQid string, err error) {
   //: mark iNode defunct
   //: iUid has iNode
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("DropNode: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   aQid = iNode //todo set properly
   if aUser.Nodes[iNode].Qid != aQid {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("DropNode: iNode %s invalid", iNode)}
   }
   if aUser.Nodes[iNode].Defunct {
      return aQid, nil
   }
   if aUser.NonDefunctNodesCount <= 1 {
      return "", &tUdbError{id: eErrLastNode, msg: "DropNode: cannot drop last node"}
   }

   aUser.Nodes[iNode] = tNode{Defunct: true, Qid: iNode}
   aUser.NonDefunctNodesCount--

   o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return aQid, nil
}

func (o *tUserDb) AddAlias(iUid, iNode, iNat, iEn string) error {
   //: add aliases to iUid and o.alias
   //: iUid has iNode
   //: iNat != iEn, iNat or iEn != ""

   if iNat == iEn {
      return &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddAlias: iNat & iEn both %s", iNat)}
   }

   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("AddAlias: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   aQid := iNode
   if aUser.Nodes[iNode].Qid != aQid {
      return &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("AddAlias: iNode %s invalid", iNode)}
   }

   aAliases := [...]string{iNat, iEn}

   aAddedCount := 0
   for _, aAlias := range aAliases {
      aUid := iUid
      if aAlias != "" {
         aUid, _ = o.Lookup(aAlias)
      }
      if aUid == iUid {
         aAddedCount++
      } else if aUid != "" {
         return &tUdbError{id: eErrAliasTaken, msg: fmt.Sprintf("AddAlias: alias %s already taken", aAlias)}
      }
   }
   if aAddedCount == 2 {
      return nil
   }

   o.aliasDoor.Lock()
   for _, aAlias := range aAliases {
      if aAlias != "" {
         o.alias[aAlias] = iUid
         err = o.putRecord(eTalias, aAlias, iUid)
         if err != nil { panic(err) }
      }
   }
   o.aliasDoor.Unlock()

   aUser.Aliases = append(aUser.Aliases, tAlias{En: iEn, Nat: iNat})
   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }

   return nil
}

func (o *tUserDb) DropAlias(iUid, iNode, iAlias string) error {
   //: mark alias defunct in o.alias
   //: iUid has iNode
   //: iAlias for iUid

   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("DropAlias: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   aQid := iNode //todo set properly
   if aUser.Nodes[iNode].Qid != aQid {
      return &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("DropAlias: iNode %s invalid", iNode)}
   }

   // check for retry
   for _, aAlias := range aUser.Aliases {
      if iAlias == aAlias.Nat && aAlias.NatDefunct ||
         iAlias == aAlias.En && aAlias.EnDefunct {
         return nil
      }
   }

   aUid, _ := o.Lookup(iAlias)
   if aUid != iUid {
      return &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("DropAlias: iAlias %s not for iUid %s", iAlias, iUid)}
   }

   o.aliasDoor.Lock()
   o.alias[iAlias] = kAliasDefunctUid
   err = o.putRecord(eTalias, iAlias, kAliasDefunctUid)
   if err != nil { panic(err) }
   o.aliasDoor.Unlock()

   for a, _ := range aUser.Aliases {
      if iAlias == aUser.Aliases[a].Nat {
         aUser.Aliases[a].NatDefunct = true
         break
      }
      if iAlias == aUser.Aliases[a].En {
         aUser.Aliases[a].EnDefunct = true
         break
      }
   }
   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return nil
}

//func (o *tUserDb) DropUser(iUid string) error { return nil }

func (o *tUserDb) Verify(iUid, iNode string) (aQid string, err error) {
   //: return Qid of node
   //: iUid has iNode
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("Verify: iUid %s not found", iUid)}
   }

   aUser.RLock()
   defer aUser.RUnlock()

   if aUser.Nodes[iNode].Defunct {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("Verify: iNode %s defunct", iNode)}
   }
   aQid = iNode //todo set properly
   if aUser.Nodes[iNode].Qid != aQid {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("Verify: iNode %s invalid", iNode)}
   }
   return aQid, nil
}

func (o *tUserDb) OpenNodes(iUid string) (aQids []string, err error) {
   //: return Qids for iUid
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return aQids, &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("OpenNodes: iUid %s not found", iUid)}
   }

   aUser.RLock()

   for _, aNode := range aUser.Nodes {
      if !aNode.Defunct {
         aQids = append(aQids, aNode.Qid)
      }
   }
   return aQids, nil
}

func (o *tUserDb) CloseNodes(iUid string) error {
   //: done with nodes
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { panic(err) }

   if aUser == nil {
      return &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("CloseNodes: iUid %s not found", iUid)}
   }

   aUser.RUnlock()
   return nil
}

func (o *tUserDb) Lookup(iAlias string) (aUid string, err error) {
   //: return uid for iAlias
   if iAlias == "" {
      return "", &tUdbError{id: eErrArgument, msg: "Lookup: iAlias is empty"}
   }

   o.aliasDoor.RLock()
   aUid = o.alias[iAlias] // check cache
   o.aliasDoor.RUnlock()

   if aUid == "" { // iAlias not in cache
      aObj, err := o.getRecord(eTalias, iAlias)
      if err != nil { panic(err) }

      if aObj == nil {
         return "", &tUdbError{id: eErrUnknownAlias, msg: fmt.Sprintf("Lookup: iAlias %s not found", iAlias)}
      }

      o.aliasDoor.Lock()
      if aTemp := o.alias[iAlias]; aTemp != "" { // recheck the map
         aUid = aTemp
      } else {
         aUid = aObj.(string)
         o.alias[iAlias] = aUid // add Uid to map
      }
      o.aliasDoor.Unlock()
   }
   return aUid, nil
}

func (o *tUserDb) GroupInvite(iGid, iAlias, iByAlias, iByUid string) (aUid string, err error) {
   //: add member to group, possibly create group
   //: iAlias exists
   //: iGid exists, iByUid in group, iByAlias ignored
   //: iGid !exists, make iGid and add iByUid with iByAlias
   //: iByAlias for iByUid
   return aUid, nil
}

func (o *tUserDb) GroupJoin(iGid, iUid, iNewAlias string) (aAlias string, err error) {
   //: set joined status for member
   //: iUid in group
   //: iNewAlias optional for iUid
   return aAlias, nil
}

func (o *tUserDb) GroupAlias(iGid, iUid, iNewAlias string) (aAlias string, err error) {
   //: update member alias
   //: iUid in group
   //: iNewAlias for iUid
   return aAlias, nil
}

func (o *tUserDb) GroupDrop(iGid, iAlias, iByUid string) (aUid string, err error) {
   //: change member status of member with iUid
   //: iAlias in group, iByUid same or in group
   //: iAlias -> iByUid, status=invited
   //: otherwise, if iAlias status==joined, status=barred else delete member
   return aUid, nil
}

func (o *tUserDb) GroupGetUsers(iGid, iByUid string) (aUids []string, err error) {
   //: return uids in iGid
   //: iByUid is member
   return aUids, nil
}

func (*tUserDb) TempUser(iUid, iNewNode string) {}
func (*tUserDb) TempAlias(iUid, iNewAlias string) {}
func (*tUserDb) TempGroup(iGid, iUid, iAlias string) {}

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

func (o *tUserDb) fetchGroup(iGid string, iMake tFetch) (*tGroup, error) {
   o.groupDoor.RLock() // read-lock group map
   aGroup := o.group[iGid] // lookup group in map
   o.groupDoor.RUnlock()

   if aGroup == nil { // group not in cache
      aObj, err := o.getRecord(eTgroup, iGid)
      if err != nil { return nil, err }
      aGroup = aObj.(*tGroup) // "type assertion" to extract *tGroup value

      if aGroup.Uid == nil { // group does not exist
         if !iMake {
            return nil, nil
         }
         aGroup.Uid = make(map[string]tMember) // initialize map of members
      }

      o.groupDoor.Lock()
      if aTemp := o.group[iGid]; aTemp != nil { // recheck the map
         aGroup = aTemp
      } else {
         o.group[iGid] = aGroup // add group to map
      }
      o.groupDoor.Unlock()
   }
   return aGroup, nil
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
      return aLn, nil
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
