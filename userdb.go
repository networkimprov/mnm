// Copyright 2017, 2018 Liam Breck
// Published at https://github.com/networkimprov/mnm
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/

package main

import (
   "hash/crc32"
   "fmt"
   "io/ioutil"
   "encoding/json"
   "os"
   "strings"
   "sync"
   "unicode"
   "net/url"
   "unicode/utf8"
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

   algrDoor sync.RWMutex
   alias map[string]string // value is Uid
   group map[string]*tGroup
}

type tUser struct {
   sync.RWMutex
   Nodes map[string]tNode
   NonDefunctNodesCount int
   Aliases []tAlias // public names for the user
   Authentication map[string]interface{} `json:",omitempty"`
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
      err = os.MkdirAll(iPath +"/"+ string(aDir), 0700)
      if err != nil { return nil, err }
   }

   aDb := new(tUserDb)
   aDb.root = iPath +"/"
   aDb.temp = aDb.root +"temp/"
   aDb.user = make(map[string]*tUser)
   aDb.alias = make(map[string]string)
   aDb.group = make(map[string]*tGroup)

   aFd, err := os.Open(aDb.temp)
   if err != nil { return aDb, err }
   aTmps, err := aFd.Readdirnames(0)
   aFd.Close()
   if err != nil { return aDb, err }
   for a := range aTmps {
      if strings.HasSuffix(aTmps[a], ".tmp") {
         err = os.Remove(aDb.temp + aTmps[a])
         if err != nil && !os.IsNotExist(err) { return aDb, err }
      } else {
         aPair := strings.SplitN(aTmps[a], "_", 2)
         if len(aPair) == 2 && tType(aPair[0]) != eTuser {
            aPair[1], err = url.QueryUnescape(aPair[1])
         }
         if len(aPair) != 2 || err != nil {
            fmt.Fprintf(os.Stderr, "NewUserDb: unexpected file %s%s\n", aDb.temp, aTmps[a])
            continue
         }
         err = aDb.complete(tType(aPair[0]), aPair[1], nil)
         if err != nil { return aDb, err }
      }
   }

   return aDb, nil
}

//: below is the public api
//: if same parameters are retried after success, ie data already exists,
//:   function should do nothing but return success

func (o *tUserDb) AddUser(iUid, iNewNode string, iAuth map[string]interface{}) (aQid string, err error) {
   //: add user
   //: iUid not in o.user, or already has iNewNode
   aUser, err := o.fetchUser(iUid, eFetchMake)
   if err != nil { return "", err }

   aUser.Lock(); defer aUser.Unlock()

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
   aUser.Authentication = iAuth

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { return "", err }
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

   aUser.Lock(); defer aUser.Unlock()

   if aUser.Nodes[iNewNode].Num != 0 {
      return "", &tUdbError{id: eErrNodeInvalid, msg: fmt.Sprintf("AddNode: Node %s exists", iNewNode)}
   }
   if aUser.NonDefunctNodesCount == kUserNodeMax {
      return "", &tUdbError{id: eErrMaxNodes, msg: fmt.Sprintf("AddNode: Exceeds %d nodes", kUserNodeMax)}
   }

   aUser.Nodes[iNewNode] = tNode{Defunct: false, Num: uint8(len(aUser.Nodes))+1}
   aUser.NonDefunctNodesCount++
   aUser.clearTouched()

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { return "", err }
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

   aUser.Lock(); defer aUser.Unlock()

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

   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { return "", err }
   return aQid, nil
}

func (o *tUserDb) AddAlias(iUid, iNat, iEn string) error {
   //: add aliases to iUid and o.alias
   //: iNat != iEn, iNat or iEn != ""
   if iNat == iEn {
      if iNat == "" {
         return &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddAlias: empty strings", iNat)}
      }
      iNat = ""
   }
   if invalidInput(iNat) {
      return &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddAlias: invalid string '%s'", iNat)}
   }
   if invalidAscii(iEn) {
      return &tUdbError{id: eErrArgument, msg: fmt.Sprintf("AddAlias: invalid string '%s'", iEn)}
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
         _, err = o.fetchGroup(aAlias, eFetchCheck) // retrieve from disk, if nec
         if err != nil { return err }
         aUid, _ = o.Lookup(aAlias) //todo return non-tUdbError
      }
      if aUid == iUid {
         aAddedCount++
      }
   }
   if aAddedCount == 2 {
      return nil
   }

   aUser.Lock(); defer aUser.Unlock()

   o.algrDoor.Lock(); defer o.algrDoor.Unlock()

   for _, aAlias := range aAliases {
      if aAlias == "" { continue }
      if o.alias[aAlias] != "" && o.alias[aAlias] != iUid {
         return &tUdbError{id: eErrAliasTaken, msg: fmt.Sprintf("AddAlias: alias %s already taken", aAlias)}
      } else if o.group[aAlias] != nil {
         return &tUdbError{id: eErrAliasTaken, msg: fmt.Sprintf("AddAlias: alias %s not available", aAlias)}
      }
   }
   if iNat != "" { o.alias[iNat] = iUid }
   if iEn  != "" { o.alias[iEn ] = iUid }

   aUser.clearTouched()
   aUser.Aliases = append(aUser.Aliases, tAlias{En:  iEn,  EnTouched:  iEn  != "",
                                                Nat: iNat, NatTouched: iNat != ""})
   err = o.putRecord(eTuser, iUid, aUser)
   if err != nil { return err }
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

   aUser.Lock(); defer aUser.Unlock()

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

   o.algrDoor.Lock()
   o.alias[iAlias] = kAliasDefunctUid
   o.algrDoor.Unlock()

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
   if err != nil { return err }
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

   aUser.RLock(); defer aUser.RUnlock()

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

   o.algrDoor.RLock()
   aUid = o.alias[iAlias] // check cache
   o.algrDoor.RUnlock()

   if aUid == "" { // iAlias not in cache
      aObj, err := o.getRecord(eTalias, iAlias)
      if err != nil { return "", err }

      if aObj == nil {
         return "", &tUdbError{id: eErrUnknownAlias, msg: fmt.Sprintf("Lookup: iAlias %s not found", iAlias)}
      }

      o.algrDoor.Lock()
      if aTemp := o.alias[iAlias]; aTemp != "" { // recheck the map
         aUid = aTemp
      } else {
         aUid = aObj.(string)
         o.alias[iAlias] = aUid // add Uid to map
      }
      o.algrDoor.Unlock()
   }
   return aUid, nil
}

func (o *tUserDb) GroupInvite(iGid, iAlias, iByAlias, iByUid string) (aUid string, err error) {
   //: add member to group, possibly create group
   //: iAlias exists
   //: iGid exists, iByUid in group, iByAlias ignored
   //: iGid !exists, make iGid and add iByUid with iByAlias
   //: iByAlias for iByUid
   aByUid, _ := o.Lookup(iByAlias)
   if aByUid == "" || aByUid != iByUid {
      return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupInvite: iByAlias %s not for iByUid %s", iByAlias, iByUid)}
   }
   aUid, _ = o.Lookup(iAlias)
   if aUid == "" {
      return "", &tUdbError{id: eErrAliasInvalid, msg: fmt.Sprintf("GroupInvite: iAlias %s not found", iAlias)}
   }

   aGroup, err := o.fetchGroup(iGid, eFetchMake)
   if err != nil { return "", err }

   aGroup.Lock(); defer aGroup.Unlock()

   if len(aGroup.Uid) == 0 {
      _, _ = o.Lookup(iGid) // retrieve from disk, if nec //todo return non-tUdbError
      o.algrDoor.Lock()
      if o.alias[iGid] != "" || invalidInput(iGid) {
         delete(o.group, iGid)
         o.algrDoor.Unlock()
         if o.alias[iGid] != "" {
            return "", &tUdbError{id: eErrAliasTaken, msg: fmt.Sprintf("GroupInvite: gid %s not available", iGid)}
         }
         return "", &tUdbError{id: eErrArgument, msg: fmt.Sprintf("GroupInvite: invalid string '%s'", iGid)}
      }
      o.algrDoor.Unlock()
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
   if err != nil { return "", err }
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

   aGroup.Lock(); defer aGroup.Unlock()

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
   if err != nil { return "", err }
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

   aGroup.Lock(); defer aGroup.Unlock()

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
   if err != nil { return "", err }
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

   aGroup.Lock(); defer aGroup.Unlock()

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
   if err != nil { return "", err }
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

   aGroup.RLock(); defer aGroup.RUnlock()

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

func (o *tUserDb) TempNode(iUid, iNewNode string) {
   aUser := o.user[iUid]
   aUser.Nodes[iNewNode] = tNode{Num:2}
   aUser.NonDefunctNodesCount = 2
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

func invalidInput(i string) bool {
   for a, aStep := 0, 0; a < len(i); a += aStep {
      var aR rune
      aR, aStep = utf8.DecodeRuneInString(i[a:])
      if aR == utf8.RuneError || !unicode.IsPrint(aR) {
         return true
      }
   }
   return false
}

func invalidAscii(i string) bool {
   for _, a := range i {
      if !(a >= 0x20 && a <= 0x7E) {
         return true
      }
   }
   return false
}

func (o *tUserDb) fileName(iT tType, iN string) string {
   if iT != eTuser {
      iN = url.QueryEscape(iN)
   }
   return o.root + string(iT) +"/"+ iN
}

func (o *tUserDb) fileTemp(iT tType, iN string) string {
   if iT != eTuser {
      iN = url.QueryEscape(iN)
   }
   return o.temp + string(iT) +"_"+ iN
}

type tFetch bool
const eFetchCheck, eFetchMake tFetch = false, true

// retrieve user from cache or disk
func (o *tUserDb) fetchUser(iUid string, iMake tFetch) (*tUser, error) {
   for _, a := range iUid {
      if !(a >= 'A' && a <= 'Z' || a >= '0' && a <= '9' || a == '+' || a == '%' ||
           a >= 'a' && a <= 'z') { // lowercase used in testing
         return nil, &tUdbError{id: eErrArgument, msg: fmt.Sprintf("fetchUser: invalid string '%s'", iUid)}
      }
   }
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
   o.algrDoor.RLock() // read-lock group map
   aGroup := o.group[iGid] // lookup group in map
   o.algrDoor.RUnlock()

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

      o.algrDoor.Lock()
      if aTemp := o.group[iGid]; aTemp != nil { // recheck the map
         aGroup = aTemp
      } else {
         o.group[iGid] = aGroup // add group to map
      }
      o.algrDoor.Unlock()
   }
   return aGroup, nil
}

// pull a file into a cache object
func (o *tUserDb) getRecord(iType tType, iId string) (interface{}, error) {
   if iType == eTalias {
      aLn, err := os.Readlink(o.fileName(iType, iId))
      if err != nil {
         if !os.IsNotExist(err) { panic(err) }
         return nil, nil
      }
      return aLn, nil
   }
   var aObj interface{}
   var aSum *uint32

   switch iType {
   case eTuser:  aObj = &tUser{};  aSum = & aObj.(*tUser).CheckSum
   case eTgroup: aObj = &tGroup{}; aSum = & aObj.(*tGroup).CheckSum
   default:      panic("getRecord: unexpected type "+iType)
   }

   aBuf, err := ioutil.ReadFile(o.fileName(iType, iId))
   if err != nil {
      if !os.IsNotExist(err) { panic(err) }
      return aObj, nil
   }
   err = json.Unmarshal(aBuf, aObj)
   if err != nil {
      return nil, &tUdbError{id: eErrChecksum, msg: fmt.Sprintf("unmarshal failed for %s/%s: %s", string(iType), iId, err.Error())}
   }
   aSumPrev := *aSum
   *aSum = 0
   aBuf, err = json.Marshal(aObj)
   if err != nil { panic(err) }

   if checkSum(aBuf) != aSumPrev {
      return nil, &tUdbError{id: eErrChecksum, msg: fmt.Sprintf("checksum failed for %s/%s", string(iType), iId)}
   }
   return aObj, nil
}

// save cache object to disk
func (o *tUserDb) putRecord(iType tType, iId string, iObj interface{}) error {
   var aSum *uint32

   switch iType {
   case eTuser:  aSum = & iObj.(*tUser).CheckSum
   case eTgroup: aSum = & iObj.(*tGroup).CheckSum
   default:      panic("putRecord: unexpected type "+iType)
   }
   *aSum = 0
   aBuf, err := json.Marshal(iObj)
   if err != nil { panic(err) }
   *aSum = checkSum(aBuf)

   aTemp := o.fileTemp(iType, iId)

   aFd, err := os.OpenFile(aTemp +".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
   if err != nil { panic(err) }
   err = json.NewEncoder(aFd).Encode(iObj)
   if err != nil { panic(err) }
   err = aFd.Sync()
   if err != nil { panic(err) }
   aFd.Close()

   err = os.Link(aTemp +".tmp", aTemp)
   if err != nil { panic(err) }
   err = syncDir(o.temp) // transaction completes at startup if we crash after this
   if err != nil { panic(err) }
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
   aPath := o.fileName(iType, iId)
   aTemp := o.fileTemp(iType, iId)

   err := os.Remove(aPath)
   if err != nil && !os.IsNotExist(err) { panic(err) }
   err = os.Link(aTemp, aPath)
   if err != nil { panic(err) }
   err = syncDir(o.root + string(iType))
   if err != nil { panic(err) }

   if iType == eTuser {
      if iObj == nil {
         iObj = &tUser{}
         var aBuf []byte
         aBuf, err = ioutil.ReadFile(aPath)
         if err != nil { panic(err) }
         err = json.Unmarshal(aBuf, iObj)
         if err != nil { panic(err) }
      }
      aSync := false
      fLink := func(cFile string, cDfn bool) {
         cPath := o.fileName(eTalias, cFile)
         cUid := iId; if cDfn { cUid = kAliasDefunctUid }
         err = os.Remove(cPath)
         if err != nil && !os.IsNotExist(err) { panic(err) }
         err = os.Symlink(cUid, cPath)
         if err != nil { panic(err) }
         aSync = true
      }
      for _, aAlias := range iObj.(*tUser).Aliases {
         if aAlias.EnTouched  { fLink(aAlias.En,  aAlias.EnDefunct ) }
         if aAlias.NatTouched { fLink(aAlias.Nat, aAlias.NatDefunct) }
      }
      if aSync {
         err = syncDir(o.root + string(eTalias))
         if err != nil { panic(err) }
      }
   }

   err = os.Remove(aTemp)
   if err != nil { panic(err) }
   err = os.Remove(aTemp +".tmp")
   if err != nil && !os.IsNotExist(err) { panic(err) }
   return nil
}
