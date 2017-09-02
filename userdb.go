// Copyright 2017 Liam Breck
//
// This file is part of the "mnm" software. Anyone may redistribute mnm and/or modify
// it under the terms of the GNU Lesser General Public License version 3, as published
// by the Free Software Foundation. See www.gnu.org/licenses/
// Mnm is distributed WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See said License for details.

package main

import (
   "hash/crc32"
   "fmt"
   "io/ioutil"
   "encoding/json"
   "os"
   "strings"
   "sync"
)

const kUserNodeMax = 100
const kAliasDefunctUid = "*defunct"

var sCrc32c = crc32.MakeTable(crc32.Castagnoli)

func checkSum(iBuf []byte) uint32 { return crc32.Checksum(iBuf, sCrc32c) }


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
   CheckSum uint32
}

type tNode struct {
  Defunct bool
  Num uint8
}

type tAlias struct {
   En string // in english
   Nat string // in whatever language
   EnDefunct, NatDefunct bool
   EnTouched, NatTouched bool // requires index update
}

func (o *tUser) clearTouched() {
   for a, _ := range o.Aliases {
      o.Aliases[a].EnTouched, o.Aliases[a].NatTouched = false, false
   }
}

func qid(iUid string, iNum uint8) string {
   return fmt.Sprintf("%s.%02x", iUid, iNum)
}

type tGroup struct {
   sync.RWMutex
   Uid map[string]tMember
   CheckSum uint32
}

type tMember struct {
   Alias string // invited/joined by this alias
   Status int8
}
const ( _=iota; eStatInvited; eStatJoined; eStatBarred )

type tUdbError struct {
   msg string
   id int
}
func (o *tUdbError) Error() string { return string(o.msg) }

const (
   _=iota;
   eErrChecksum;
   eErrArgument;
   eErrMissingNode;
   eErrUserInvalid; eErrMaxNodes; eErrNodeInvalid; eErrLastNode;
   eErrUnknownAlias; eErrAliasTaken; eErrAliasInvalid;
   eErrMemberJoined; eErrGroupInvalid;
)

type tType string
const (
   eTuser  tType = "user"
   eTalias tType = "alias"
   eTgroup tType = "group"
)


func NewUserDb(iPath string) (*tUserDb, error) {
   var err error
   for _, aDir := range [...]tType{ "temp", eTuser, eTalias, eTgroup } {
      err = os.MkdirAll(iPath + "/" + string(aDir), 0700)
      if err != nil { return nil, err }
   }

   aDb := new(tUserDb)
   aDb.root = iPath+"/"
   aDb.temp = aDb.root + "temp/"
   aDb.user = make(map[string]*tUser)
   aDb.alias = make(map[string]string)
   aDb.group = make(map[string]*tGroup)

   aFd, err := os.Open(aDb.temp)
   if err != nil { return aDb, err }
   aTmps, err := aFd.Readdirnames(0)
   aFd.Close()
   if err != nil { return aDb, err }
   for _, aTmp := range aTmps {
      if strings.HasSuffix(aTmp, ".tmp") {
         err = os.Remove(aDb.temp + aTmp)
         if err != nil { return aDb, err }
      } else {
         aPair := strings.SplitN(aTmp, "_", 2)
         if len(aPair) != 2 {
            fmt.Fprintf(os.Stderr, "NewUserDb: unexpected file %s%s\n", aDb.temp, aTmp)
         } else {
            err = aDb.complete(tType(aPair[0]), aPair[1], nil)
            if err != nil { return aDb, err }
         }
      }
   }

   return aDb, nil
}

//: below is the public api
//: if same parameters are retried after success, ie data already exists,
//:   function should do nothing but return success

func (o *tUserDb) AddUser(iUid, iNewNode string) (aQid string, err error) {
   //: add user
   //: iUid not in o.user, or already has iNewNode
   aUser, err := o.fetchUser(iUid, eFetchMake)
   if err != nil { return "", err }

   aUser.Lock()
   defer aUser.Unlock()

   aQid = qid(iUid, 1)

   if len(aUser.Nodes) != 0 {
      if aUser.Nodes[iNewNode].Num != 1 {
         return "", &tUdbError{id: eErrMissingNode,
                       msg: fmt.Sprintf("AddUser: Uid %s found, Node %s missing", iUid, iNewNode)}
      }
      return aQid, nil
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Num: 1}
   aUser.NonDefunctNodesCount++

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return aQid, nil
}

func (o *tUserDb) AddNode(iUid, iNewNode string) (aQid string, err error) {
   //: add node
   //: iUid may already have iNewNode
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return "", err }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("AddNode: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   if aUser.Nodes[iNewNode].Num != 0 {
      return qid(iUid, aUser.Nodes[iNewNode].Num), nil
   }
   if aUser.NonDefunctNodesCount == kUserNodeMax {
      return "", &tUdbError{id: eErrMaxNodes, msg: fmt.Sprintf("AddNode: Exceeds %d nodes", kUserNodeMax)}
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Num: uint8(len(aUser.Nodes))+1}
   aUser.NonDefunctNodesCount++
   aUser.clearTouched()

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return qid(iUid, aUser.Nodes[iNewNode].Num), nil
}

func (o *tUserDb) DropNode(iUid, iNode string) (aQid string, err error) {
   //: mark iNode defunct
   //: iUid has iNode
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return "", err }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("DropNode: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

   if aUser.Nodes[iNode].Num == 0 {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("DropNode: iNode %s invalid", iNode)}
   }
   aQid = qid(iUid, aUser.Nodes[iNode].Num)
   if aUser.Nodes[iNode].Defunct {
      return aQid, nil
   }
   if aUser.NonDefunctNodesCount <= 1 {
      return "", &tUdbError{id: eErrLastNode, msg: "DropNode: cannot drop last node"}
   }

   aUser.Nodes[iNode] = tNode{Defunct: true, Num: aUser.Nodes[iNode].Num}
   aUser.NonDefunctNodesCount--
   aUser.clearTouched()

   o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }
   return aQid, nil
}

func (o *tUserDb) AddAlias(iUid, iNat, iEn string) error {
   //: add aliases to iUid and o.alias
   //: iNat != iEn, iNat or iEn != ""
   if iNat == iEn {
      return &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddAlias: iNat & iEn both %s", iNat)}
   }

   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return err }

   if aUser == nil {
      return &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("AddAlias: iUid %s not found", iUid)}
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
      }
   }
   if aAddedCount == 2 {
      return nil
   }

   aUser.Lock()
   defer aUser.Unlock()

   o.aliasDoor.Lock()
   defer o.aliasDoor.Unlock()

   for _, aAlias := range aAliases {
      if aAlias != "" && o.alias[aAlias] != "" && o.alias[aAlias] != iUid {
         return &tUdbError{id: eErrAliasTaken, msg: fmt.Sprintf("AddAlias: alias %s already taken", aAlias)}
      }
   }
   if iNat != "" { o.alias[iNat] = iUid }
   if iEn  != "" { o.alias[iEn ] = iUid }

   aUser.clearTouched()
   aUser.Aliases = append(aUser.Aliases, tAlias{En:  iEn,  EnTouched:  iEn  != "",
                                                Nat: iNat, NatTouched: iNat != ""})
   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { panic(err) }

   return nil
}

func (o *tUserDb) DropAlias(iUid, iAlias string) error {
   //: mark alias defunct in o.alias
   //: iAlias for iUid
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return err }

   if aUser == nil {
      return &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("DropAlias: iUid %s not found", iUid)}
   }

   aUser.Lock()
   defer aUser.Unlock()

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
   o.aliasDoor.Unlock()

   aUser.clearTouched()
   for a, _ := range aUser.Aliases {
      if iAlias == aUser.Aliases[a].Nat {
         aUser.Aliases[a].NatDefunct = true
         aUser.Aliases[a].NatTouched = true
         break
      }
      if iAlias == aUser.Aliases[a].En {
         aUser.Aliases[a].EnDefunct = true
         aUser.Aliases[a].EnTouched = true
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
   if err != nil { return "", err }

   if aUser == nil {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("Verify: iUid %s not found", iUid)}
   }

   aUser.RLock()
   defer aUser.RUnlock()

   if aUser.Nodes[iNode].Defunct {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("Verify: iNode %s defunct", iNode)}
   }
   if aUser.Nodes[iNode].Num == 0 {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("Verify: iNode %s invalid", iNode)}
   }
   return qid(iUid, aUser.Nodes[iNode].Num), nil
}

func (o *tUserDb) OpenNodes(iUid string) (aQids []string, err error) {
   //: return Qids for iUid
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return nil, err }

   if aUser == nil {
      return nil, &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("OpenNodes: iUid %s not found", iUid)}
   }

   aUser.RLock()

   for _, aNode := range aUser.Nodes {
      if !aNode.Defunct {
         aQids = append(aQids, qid(iUid, aNode.Num))
      }
   }
   return aQids, nil
}

func (o *tUserDb) CloseNodes(iUid string) error {
   //: done with nodes
   aUser, err := o.fetchUser(iUid, eFetchCheck)
   if err != nil { return err }

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
   aUid, _ = o.Lookup(iAlias)
   if aUid == "" {
      return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupInvite: iAlias %s not found", iAlias)}
   }

   aGroup, err := o.fetchGroup(iGid, eFetchMake)
   if err != nil { return "", err }

   aGroup.Lock()
   defer aGroup.Unlock()

   if len(aGroup.Uid) == 0 {
      aByUid, _ := o.Lookup(iByAlias)
      if aByUid == "" || aByUid != iByUid {
         o.groupDoor.Lock()
         delete(o.group, iGid)
         o.groupDoor.Unlock()

         if aByUid == "" {
            return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupInvite: iByAlias %s not found", iByAlias)}
         } else {
            return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupInvite: iByAlias %s not for iByUid %s", iByAlias, iByUid)}
         }
      }
      aGroup.Uid[iByUid] = tMember{Alias: iByAlias, Status: eStatJoined}
   } else {
      if aGroup.Uid[iByUid].Status != eStatJoined {
         return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("GroupInvite: iByUid %s not a member", iByUid)}
      }
      if aGroup.Uid[aUid].Status == eStatInvited {
         return aUid, nil
      }
      if aGroup.Uid[aUid].Status == eStatJoined {
         return "", &tUdbError{id: eErrMemberJoined, msg: fmt.Sprintf("GroupInvite: iAlias %s already joined", iAlias)}
      }
   }
   aGroup.Uid[aUid] = tMember{Alias: iAlias, Status: eStatInvited}

   err = o.putRecord(eTgroup, iGid, aGroup)
   if err != nil { panic(err) }
   return aUid, nil
}

func (o *tUserDb) GroupJoin(iGid, iUid, iNewAlias string) (aAlias string, err error) {
   //: set joined status for member
   //: iUid in group
   //: iNewAlias optional for iUid

   aGroup, err := o.fetchGroup(iGid, eFetchCheck)
   if err != nil { return "", err }

   if aGroup == nil {
      return "", &tUdbError{id: eErrGroupInvalid, msg: fmt.Sprintf("GroupJoin: iGid %s not found", iGid)}
   }

   aGroup.Lock()
   defer aGroup.Unlock()

   if aGroup.Uid[iUid].Status == eStatJoined &&
      (iNewAlias == "" || iNewAlias == aGroup.Uid[iUid].Alias) {
      return aGroup.Uid[iUid].Alias, nil
   }
   if aGroup.Uid[iUid].Status != eStatInvited && aGroup.Uid[iUid].Status != eStatJoined {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("GroupJoin: iUid %s not invited", iUid)}
   }

   if iNewAlias != "" {
      aUid, _ := o.Lookup(iNewAlias)
      if aUid != iUid {
         return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupJoin: iNewAlias %s not for iUid %s", iNewAlias, iUid)}
      }
      aAlias = iNewAlias
   } else {
      aAlias = aGroup.Uid[iUid].Alias
   }
   aGroup.Uid[iUid] = tMember{Alias: aAlias, Status: eStatJoined}

   err = o.putRecord(eTgroup, iGid, aGroup)
   if err != nil { panic(err) }
   return aAlias, nil
}

func (o *tUserDb) GroupAlias(iGid, iUid, iNewAlias string) (aAlias string, err error) {
   //: update member alias
   //: iUid in group
   //: iNewAlias for iUid
   aGroup, err := o.fetchGroup(iGid, eFetchCheck)
   if err != nil { return "", err }

   if aGroup == nil {
      return "", &tUdbError{id: eErrGroupInvalid, msg: fmt.Sprintf("GroupAlias: iGid %s not found", iGid)}
   }

   aGroup.Lock()
   defer aGroup.Unlock()

   if aGroup.Uid[iUid].Status != eStatJoined {
      return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("GroupAlias: iUid %s not a member", iUid)}
   }
   if iNewAlias == aGroup.Uid[iUid].Alias {
      return iNewAlias, nil
   }
   aUid, _ := o.Lookup(iNewAlias)
   if aUid != iUid {
      return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupAlias: iNewAlias %s not for iUid %s", iNewAlias, iUid)}
   }
   aAlias = aGroup.Uid[iUid].Alias
   aGroup.Uid[iUid] = tMember{Alias: iNewAlias, Status: aGroup.Uid[iUid].Status}

   err = o.putRecord(eTgroup, iGid, aGroup)
   if err != nil { panic(err) }
   return aAlias, nil
}

func (o *tUserDb) GroupQuit(iGid, iAlias, iByUid string) (aUid string, err error) {
   //: change member status of member with iUid
   //: iAlias in group, iByUid same or in group
   //: iAlias -> iByUid, status=invited
   //: otherwise, if iAlias status==joined, status=barred else delete member
   aGroup, err := o.fetchGroup(iGid, eFetchCheck)
   if err != nil { return "", err }

   if aGroup == nil {
      return "", &tUdbError{id: eErrGroupInvalid, msg: fmt.Sprintf("GroupQuit: iGid %s not found", iGid)}
   }

   aGroup.Lock()
   defer aGroup.Unlock()

   aUid, _ = o.Lookup(iAlias)
   if aUid == "" || iAlias != aGroup.Uid[aUid].Alias {
      return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupQuit: iAlias %s not a member", iAlias)}
   }

   if iByUid == aUid {
      if aGroup.Uid[aUid].Status == eStatInvited {
         return aUid, nil
      }
      aGroup.Uid[aUid] = tMember{Status: eStatInvited, Alias: iAlias}
   } else {
      if aGroup.Uid[iByUid].Status != eStatJoined {
         return "", &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("GroupQuit: iByUid %s not a member", iByUid)}
      }
      if aGroup.Uid[aUid].Status == eStatBarred {
         return aUid, nil
      }
      aGroup.Uid[aUid] = tMember{Status: eStatBarred, Alias: iAlias}
   }

   err = o.putRecord(eTgroup, iGid, aGroup)
   if err != nil { panic(err) }
   return aUid, nil
}

func (o *tUserDb) GroupGetUsers(iGid, iByUid string) (aUids []string, err error) {
   //: return uids in iGid
   //: iByUid is member
   aGroup, err := o.fetchGroup(iGid, eFetchCheck)
   if err != nil { return nil, err }

   if aGroup == nil {
      return nil, &tUdbError{id: eErrGroupInvalid, msg: fmt.Sprintf("GroupGetUsers: iGid %s not found", iGid)}
   }

   aGroup.RLock()
   defer aGroup.RUnlock()

   if aGroup.Uid[iByUid].Status != eStatJoined &&
      aGroup.Uid[iByUid].Status != eStatInvited {
      return nil, &tUdbError{id: eErrUserInvalid, msg: fmt.Sprintf("GroupGetUsers: iByUid %s not a member", iByUid)}
   }

   for aK, aV := range aGroup.Uid {
      if aV.Status == eStatJoined {
         aUids = append(aUids, aK)
      }
   }

   return aUids, nil
}

// TempXyz methods for testing use only

func (o *tUserDb) TempUser(iUid, iNewNode string) {
   o.user[iUid] = &tUser{Nodes: map[string]tNode{iNewNode: {Num:1}}, NonDefunctNodesCount:1}
}

func (o *tUserDb) TempAlias(iUid, iNewAlias string) {
   o.alias[iNewAlias] = iUid
}

func (o *tUserDb) TempGroup(iGid, iUid, iAlias string) {
   if o.group[iGid] == nil {
      o.group[iGid] = &tGroup{Uid: map[string]tMember{}}
   }
   var aS int8 = eStatJoined; if iGid == "blab" { aS = eStatInvited }
   o.group[iGid].Uid[iUid] = tMember{Alias: iAlias, Status: aS}
}

func (o *tUserDb) Erase() {
   err := os.RemoveAll(o.root)
   if err != nil { panic(err) }
}


// non-public methods follow

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
   var aSum *uint32

   switch iType {
   default:
      panic("getRecord: unexpected type "+iType)
   case eTalias:
      aLn, err := os.Readlink(aPath)
      if err != nil {
         if os.IsNotExist(err) { return nil, nil }
         panic(err)
      }
      return aLn, nil
   case eTuser:  aObj = &tUser{};  aSum = & aObj.(*tUser).CheckSum
   case eTgroup: aObj = &tGroup{}; aSum = & aObj.(*tGroup).CheckSum
   }

   aBuf, err := ioutil.ReadFile(aPath)
   if err != nil {
      if os.IsNotExist(err) { return aObj, nil }
      panic(err)
   }

   err = json.Unmarshal(aBuf, aObj)
   if err != nil {
      return nil, &tUdbError{id: eErrChecksum, msg: fmt.Sprintf("unmarshal failed for %s/%s: %s", string(iType), iId, err.Error())}
   }
   aSumPrev := *aSum
   *aSum = 0
   aBuf, _ = json.Marshal(aObj)
   if checkSum(aBuf) != aSumPrev {
      return nil, &tUdbError{id: eErrChecksum, msg: fmt.Sprintf("checksum failed for %s/%s", string(iType), iId)}
   }
   return aObj, nil
}

// save cache object to disk
func (o *tUserDb) putRecord(iType tType, iId string, iObj interface{}) error {
   var err error
   aTemp := o.temp + string(iType) + "_" + iId
   var aSum *uint32

   switch iType {
   default:
      panic("putRecord: unexpected type "+iType)
   case eTuser:  aSum = & iObj.(*tUser).CheckSum
   case eTgroup: aSum = & iObj.(*tGroup).CheckSum
   }

   *aSum = 0
   aBuf, err := json.Marshal(iObj)
   if err != nil { return err }
   *aSum = checkSum(aBuf)

   aFd, err := os.OpenFile(aTemp + ".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
   if err != nil { return err }
   defer aFd.Close()
   err = json.NewEncoder(aFd).Encode(iObj)
   if err != nil { return err }
   err = aFd.Sync()
   if err != nil { return err }

   err = os.Link(aTemp + ".tmp", aTemp)
   if err != nil { return err }
   err = syncDir(o.temp) // transaction completes at startup if we crash after this
   if err != nil { return err }
   err = o.complete(iType, iId, iObj)
   return err
}

func syncDir(iPath string) error {
   aFd, err := os.Open(iPath)
   if err != nil { return err }
   err = aFd.Sync()
   aFd.Close()
   return err
}

// move valid temp/file to data dir
func (o *tUserDb) complete(iType tType, iId string, iObj interface{}) error {
   var err error
   aPath := o.root + string(iType) + "/" + iId
   aTemp := o.temp + string(iType) + "_" + iId

   err = os.Remove(aPath)
   if err != nil && !os.IsNotExist(err) { return err }
   err = os.Link(aTemp, aPath)
   if err != nil { return err }
   err = syncDir(o.root + string(iType))
   if err != nil { return err }

   if iType == eTuser {
      if iObj == nil {
         iObj = &tUser{}
         aBuf, err := ioutil.ReadFile(aPath)
         if err != nil { return err }
         err = json.Unmarshal(aBuf, iObj)
         if err != nil { return err }
      }
      aDir := o.root + string(eTalias) + "/"
      aSync := false
      fLink := func(cFile string, cDfn bool) {
         cUid := iId; if cDfn { cUid = kAliasDefunctUid }
         err = os.Remove(aDir + cFile)
         if err != nil && !os.IsNotExist(err) { return }
         err = os.Symlink(cUid, aDir + cFile)
         aSync = true
      }
      for _, aAlias := range iObj.(*tUser).Aliases {
         if aAlias.EnTouched  { fLink(aAlias.En,  aAlias.EnDefunct ); if err != nil { return err } }
         if aAlias.NatTouched { fLink(aAlias.Nat, aAlias.NatDefunct); if err != nil { return err } }
      }
      if aSync {
         syncDir(aDir)
      }
   }

   err = os.Remove(aTemp)
   if err != nil { return err }
   err = os.Remove(aTemp + ".tmp")
   if err != nil && !os.IsNotExist(err) { return err }
   return nil
}
