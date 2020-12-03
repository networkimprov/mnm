// Copyright 2017, 2018 Liam Breck
// Published at https://github.com/networkimprov/mnm
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/

package main

import (
   "fmt"
   "io/ioutil"
   "os"
)

func TestUserDb(iPath string) bool {
   //: exercise the api, print diagnostics
   //: invoke from main() before tTestClient loop; stop program if tests fail
   _ = os.RemoveAll(iPath)

   err := os.MkdirAll(iPath + "/temp", 0700)
   if err != nil { panic(err) }
   aJson := `{"Aliases":[{"En":"complete/a1","EnTouched":true}]}`
   err = ioutil.WriteFile(iPath + "/temp/user_complete", []byte(aJson), 0600)
   if err != nil { panic(err) }
   err = ioutil.WriteFile(iPath + "/temp/user_complete.tmp", []byte("{}"), 0600)
   if err != nil { panic(err) }
   aJson = `{"Uid":{"complete":{"Alias":"complete/a1","Status":2}}}`
   err = ioutil.WriteFile(iPath + "/temp/group_complete%2Fg1", []byte(aJson), 0600)
   if err != nil { panic(err) }

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

   // COMPLETE
   _, err = os.Lstat(iPath + "/user/complete")
   if err != nil {
      fReport("complete user failed")
   }
   _, err = os.Lstat(iPath + "/alias/complete%2Fa1")
   if err != nil {
      fReport("complete alias failed")
   }
   _, err = os.Lstat(iPath + "/group/complete%2Fg1")
   if err != nil {
      fReport("complete group failed")
   }
   _, err = os.Lstat(iPath + "/temp/user_complete")
   if err == nil || !os.IsNotExist(err) {
      fReport("complete user incomplete")
   }
   _, err = os.Lstat(iPath + "/temp/user_complete.tmp")
   if err == nil || !os.IsNotExist(err) {
      fReport("complete cleanup failed")
   }
   _, err = os.Lstat(iPath + "/temp/group_complete%2Fg1")
   if err == nil || !os.IsNotExist(err) {
      fReport("complete group incomplete")
   }

   var aUid1, aUid2, aNode1, aNode2 string
   var aAlias1, aAlias2, aAlias3 string
   var aGid1, aGid2 string
   aNat := "\u30a2\u30ea\u30a2\u30b9"

   // ADDUSER
   aUid1 = "AddUserUid1"
   aNode1 = "AddUserN1"
   _, err = aDb.AddUser(aUid1, aNode1)
   if err != nil || aDb.user[aUid1].Nodes[aNode1].Num != 1 {
      fReport("add case failed")
   }
   _, err = aDb.AddUser(aUid1, aNode1)
   if err != nil || aDb.user[aUid1].Nodes[aNode1].Num != 1 {
      fReport("re-add case failed")
   }
   _, err = aDb.AddUser(aUid1, "AddUserN0")
   if err == nil || err.(*tUdbError).id != eErrMissingNode {
      fReport("add existing case succeeded: AddUser")
   }
   _, err = aDb.AddUser("AddUserUid\x00", "AddUserN0")
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("non-printable uid case succeeded: AddUser")
   }
   delete(aDb.user, aUid1) // verify checksum next read

   // ADDNODE
   aUid1, aUid2 = "AddUserUid1", "AddNodeUid2"
   aNode1 = "AddNodeN2"
   _, err = aDb.AddNode(aUid1, aNode1)
   if err != nil || aDb.user[aUid1].Nodes[aNode1].Num != 2 {
      fReport("add case failed")
   }
   _, err = aDb.AddNode(aUid1, aNode1)
   if err == nil || err.(*tUdbError).id != eErrNodeInvalid {
      fReport("re-add case failed")
   }
   _, err = aDb.AddNode("AddNodeUid0", aNode1)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: AddNode")
   }
   aDb.AddUser(aUid2, aNode1)
   for a := 1; a < 100; a++ {
      _, err = aDb.AddNode(aUid2, "AddNodeN0"+fmt.Sprint(a))
      if err != nil {
         fReport("add 100 case failed")
         break
      }
   }
   _, err = aDb.AddNode(aUid2, "AddNodeN100")
   if err == nil || err.(*tUdbError).id != eErrMaxNodes {
      fReport("add >100 case succeeded: AddNode")
   }
   delete(aDb.user, aUid2)
   aFd, _ := os.OpenFile(aDb.root + "user/" + aUid2, os.O_WRONLY, 0600)
   aFd.WriteAt([]byte{'#'}, 2)
   aFd.Close()
   _, err = aDb.AddNode(aUid2, "AddNodeN100")
   if err == nil || err.(*tUdbError).id != eErrChecksum {
      fReport("checksum case succeeded")
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
   _, err = aDb.DropNode("DropNodeUid0", aNode1)
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
   aNode1 = "AddAliasN2"
   err = aDb.AddAlias(aUid1, aNat, "AddAlias/A1")
   if err != nil || aDb.alias[aNat] != aUid1 || aDb.alias["AddAlias/A1"] != aUid1 {
      fReport("add both case failed")
   }
   err = aDb.AddAlias(aUid1, aNat, "AddAlias/A1")
   if err != nil || aDb.alias[aNat] != aUid1 || aDb.alias["AddAlias/A1"] != aUid1 {
      fReport("re-add both case failed")
   }
   err = aDb.AddAlias(aUid1, "", "AddAlias/A2")
   if err != nil || aDb.alias["AddAlias/A2"] != aUid1 {
      fReport("add en case failed")
   }
   err = aDb.AddAlias(aUid1, aNat+"2", "")
   if err != nil || aDb.alias[aNat+"2"] != aUid1 {
      fReport("add nat case failed")
   }
   err = aDb.AddAlias(aUid1, aNat+"3", "AddAlias/A2")
   if err != nil || aDb.alias[aNat+"3"] != aUid1 || aDb.alias["AddAlias/A2"] != aUid1 {
      fReport("re-add en case failed")
   }
   err = aDb.AddAlias(aUid1, aNat+"2", "AddAlias/A3")
   if err != nil || aDb.alias[aNat+"2"] != aUid1 || aDb.alias["AddAlias/A3"] != aUid1 {
      fReport("re-add nat case failed")
   }
   err = aDb.AddAlias(aUid1, "AddAlias/A3", "AddAlias/A3")
   if err != nil || aDb.alias["AddAlias/A3"] != aUid1 {
      fReport("duplicate en case failed")
   }
   err = aDb.AddAlias(aUid1, aNat, aNat)
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("duplicate nat case succeeded: AddAlias")
   }
   err = aDb.AddAlias(aUid1, "", "")
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("empty args case succeeded: AddAlias")
   }
   err = aDb.AddAlias(aUid1, "", "AddAlias/A\x00")
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("non-printable en case succeeded: AddAlias")
   }
   err = aDb.AddAlias(aUid1, aNat+"\x00", "")
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("non-printable nat case succeeded: AddAlias")
   }
   err = aDb.AddAlias(aUid1, aNat+"\xFF", "")
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("non-UTF8 nat case succeeded: AddAlias")
   }
   err = aDb.AddAlias("AddAliasUid0", aNat, "")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: AddAlias")
   }
   aDb.AddUser(aUid2, aNode1)
   err = aDb.AddAlias(aUid2, aNat, "")
   if err == nil || err.(*tUdbError).id != eErrAliasTaken {
      fReport("already taken case succeeded: AddAlias")
   }
   aDb.TempGroup("AddAliasGid1", aUid1, "AddAlias/A1")
   err = aDb.AddAlias(aUid1, "", "AddAliasGid1")
   if err == nil || err.(*tUdbError).id != eErrAliasTaken {
      fReport("already taken by group case succeeded: AddAlias")
   }

   // DROPALIAS
   aUid1, aUid2 = "AddUserUid1", "AddAliasUid2"
   err = aDb.DropAlias(aUid1, "AddAlias/A1")
   if err != nil || aDb.alias["AddAlias/A1"] != kAliasDefunctUid {
      fReport("drop en case failed")
   }
   err = aDb.DropAlias(aUid1, "AddAlias/A1")
   if err != nil || aDb.alias["AddAlias/A1"] != kAliasDefunctUid {
      fReport("re-drop en case failed")
   }
   err = aDb.DropAlias(aUid1, aNat)
   if err != nil || aDb.alias[aNat] != kAliasDefunctUid {
      fReport("drop nat case failed")
   }
   err = aDb.DropAlias("DropAliasUid0", aNat)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: DropAlias")
   }
   err = aDb.DropAlias(aUid2, "AddAlias/A2")
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

   // GROUPINVITE
   aGid1, aGid2 = "Ginvite/Gid1", "Ginvite/Gid2"
   aUid1, aUid2 = "AddUserUid1", "GinviteUid2"
   aAlias1, aAlias2 = "GinviteA1", "GinviteA2"
   aDb.AddUser(aUid2, "GinviteN2")
   aDb.AddAlias(aUid2, "", aAlias2)
   aDb.AddAlias(aUid1, "", aAlias1)
   _, err = aDb.GroupInvite(aGid1, aAlias1, aAlias2, aUid2)
   if err != nil || aDb.group[aGid1].Uid[aUid2].Status != eStatJoined ||
                    aDb.group[aGid1].Uid[aUid1].Status != eStatInvited {
      fReport("invite new case failed")
   }
   _, err = aDb.GroupInvite(aGid1, aAlias1, aAlias2, aUid2)
   if err != nil || aDb.group[aGid1].Uid[aUid2].Status != eStatJoined ||
                    aDb.group[aGid1].Uid[aUid1].Status != eStatInvited {
      fReport("re-invite case failed")
   }
   aDb.TempGroup(aGid2, aUid2, aAlias2)
   _, err = aDb.GroupInvite(aGid2, aAlias1, aAlias2, aUid2)
   if err != nil || aDb.group[aGid2].Uid[aUid1].Status != eStatInvited {
      fReport("invite existing case failed")
   }
   _, err = aDb.GroupInvite(aGid1, aAlias2, aAlias1, aUid1)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("non-member invitor case succeeded: GroupInvite")
   }
   _, err = aDb.GroupInvite(aGid1, "GinviteA0", aAlias2, aUid2)
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid invitee case succeeded: GroupInvite")
   }
   _, err = aDb.GroupInvite("Ginvite/Gid0", aAlias1, "GinviteA0", aUid2)
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid invitor alias case succeeded: GroupInvite")
   }
   _, err = aDb.GroupInvite("Ginvite/Gid0", aAlias1, aAlias2, "GinviteUid0")
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid invitor uid case succeeded: GroupInvite")
   }
   _, err = aDb.GroupInvite("Ginvite/Gid\x00", aAlias1, aAlias2, aUid2)
   if err == nil || err.(*tUdbError).id != eErrArgument {
      fReport("non-printable gid case succeeded: GroupInvite")
   }
   aDb.TempAlias(aUid2, "GinviteAlias1")
   _, err = aDb.GroupInvite("GinviteAlias1", aAlias1, aAlias2, aUid2)
   if err == nil || err.(*tUdbError).id != eErrAliasTaken {
      fReport("unavailable gid case succeeded: GroupInvite")
   }
   aDb.TempGroup(aGid2, aUid1, aAlias1)
   _, err = aDb.GroupInvite(aGid2, aAlias1, aAlias2, aUid2)
   if err == nil || err.(*tUdbError).id != eErrMemberJoined {
      fReport("already joined case succeeded: GroupInvite")
   }

   // GROUPJOIN
   aGid1, aGid2 = "GjoinGid1", "GjoinGid2"
   aUid1, aUid2 = "AddUserUid1", "GjoinUid2"
   aAlias1, aAlias2, aAlias3 = "GjoinA1", "GjoinA2", "GjoinA3"
   aDb.AddUser(aUid2, "GjoinN2")
   aDb.AddAlias(aUid2, "", aAlias2)
   aDb.AddAlias(aUid1, "", aAlias1)
   aDb.AddAlias(aUid1, "", aAlias3)
   aDb.GroupInvite(aGid1, aAlias1, aAlias2, aUid2)
   aDb.GroupInvite(aGid2, aAlias1, aAlias2, aUid2)
   _, err = aDb.GroupJoin(aGid1, aUid1, "")
   if err != nil || aDb.group[aGid1].Uid[aUid1].Status != eStatJoined {
      fReport("join case failed")
   }
   _, err = aDb.GroupJoin(aGid1, aUid1, "")
   if err != nil || aDb.group[aGid1].Uid[aUid1].Status != eStatJoined {
      fReport("re-join case failed")
   }
   _, err = aDb.GroupJoin(aGid2, aUid1, aAlias3)
   if err != nil || aDb.group[aGid2].Uid[aUid1].Status != eStatJoined ||
                    aDb.group[aGid2].Uid[aUid1].Alias != aAlias3 {
      fReport("join new-alias case failed")
   }
   _, err = aDb.GroupJoin(aGid2, aUid1, aAlias3)
   if err != nil || aDb.group[aGid2].Uid[aUid1].Status != eStatJoined ||
                    aDb.group[aGid2].Uid[aUid1].Alias != aAlias3 {
      fReport("re-join new-alias case failed")
   }
   _, err = aDb.GroupJoin(aGid1, aUid1, "GjoinA0")
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid alias case succeeded: GroupJoin")
   }
   _, err = aDb.GroupJoin("GjoinGid0", aUid1, "")
   if err == nil || err.(*tUdbError).id != eErrGroupInvalid {
      fReport("invalid group case succeeded: GroupJoin")
   }
   _, err = aDb.GroupJoin(aGid1, "GjoinUid0", "")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: GroupJoin")
   }
   delete(aDb.group, aGid1) // verify checksum next read

   // GROUPALIAS
   aGid1 = "GjoinGid1"
   aUid1 = "AddUserUid1"
   aAlias1 = "GjoinA1"
   _, err = aDb.GroupAlias(aGid1, aUid1, aAlias1)
   if err != nil || aDb.group[aGid1].Uid[aUid1].Alias != aAlias1 {
      fReport("alias case failed")
   }
   _, err = aDb.GroupAlias(aGid1, aUid1, aAlias1)
   if err != nil || aDb.group[aGid1].Uid[aUid1].Alias != aAlias1 {
      fReport("re-alias case failed")
   }
   _, err = aDb.GroupAlias(aGid1, aUid1, "GaliasA0")
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid alias case succeeded: GroupAlias")
   }
   _, err = aDb.GroupAlias("GaliasGid0", aUid1, aAlias1)
   if err == nil || err.(*tUdbError).id != eErrGroupInvalid {
      fReport("invalid group case succeeded: GroupAlias")
   }
   _, err = aDb.GroupAlias(aGid1, "GaliasUid0", aAlias1)
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid group case succeeded: GroupAlias")
   }

   // GROUPQUIT
   aGid1 = "GjoinGid1"
   aUid1, aUid2 = "AddUserUid1", "GjoinUid2"
   aAlias1 = "GjoinA1"
   var aUid string
   aUid, err = aDb.GroupQuit(aGid1, aAlias1, aUid1)
   if err != nil || aDb.group[aGid1].Uid[aUid].Status != eStatInvited {
      fReport("quit self case failed")
   }
   aUid, err = aDb.GroupQuit(aGid1, aAlias1, aUid1)
   if err != nil || aDb.group[aGid1].Uid[aUid].Status != eStatInvited {
      fReport("re-quit self case failed")
   }
   aUid, err = aDb.GroupQuit(aGid1, aAlias1, aUid2)
   if err != nil || aDb.group[aGid1].Uid[aUid].Status != eStatBarred {
      fReport("quit other case failed")
   }
   aUid, err = aDb.GroupQuit(aGid1, aAlias1, aUid2)
   if err != nil || aDb.group[aGid1].Uid[aUid].Status != eStatBarred {
      fReport("re-quit other case failed")
   }
   _, err = aDb.GroupQuit(aGid1, aAlias1, "GquitUid0")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: GroupQuit")
   }
   _, err = aDb.GroupQuit("GquitGid0", aAlias1, aUid2)
   if err == nil || err.(*tUdbError).id != eErrGroupInvalid {
      fReport("invalid group case succeeded: GroupQuit")
   }
   _, err = aDb.GroupQuit(aGid1, "GquitA0", aUid2)
   if err == nil || err.(*tUdbError).id != eErrAliasInvalid {
      fReport("invalid alias case succeeded: GroupQuit")
   }

   // GROUPGETUSERS
   aGid1 = "GjoinGid2"
   aUid1 = "AddUserUid1"
   var aUids []string
   aUids, err = aDb.GroupGetUsers(aGid1, aUid1)
   if err != nil || len(aUids) != 2 {
      fReport("getusers case failed")
   }
   _, err = aDb.GroupGetUsers(aGid1, "GgetusersUid0")
   if err == nil || err.(*tUdbError).id != eErrUserInvalid {
      fReport("invalid user case succeeded: GroupGetUsers")
   }
   _, err = aDb.GroupGetUsers("GgetusersGid0", aUid1)
   if err == nil || err.(*tUdbError).id != eErrGroupInvalid {
      fReport("invalid group case succeeded: GroupGetUsers")
   }

   if aOk {
      fmt.Println("UserDb tests passed")
   }
   return aOk
}
