// Copyright 2017 Liam Breck
//
// This file is part of the "mnm" software. Anyone may redistribute mnm and/or modify
// it under the terms of the GNU Lesser General Public License version 3, as published
// by the Free Software Foundation. See www.gnu.org/licenses/
// Mnm is distributed WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See said License for details.

package main

import (
   "fmt"
   "io/ioutil"
   "encoding/json"
   "net"
   "os"
   "mnm/qlib"
   "strconv"
   "crypto/tls"
)

const kVersionA, kVersionB, kVersionC = 0, 0, 0
const kVersionDate = "(unreleased)"
const kConfigFile = "mnm.config"

var sConfig tConfig


func main() { os.Exit(mainResult()) }

func mainResult() int {
   // return 2 reserved for use by Go internals
   var err error

   aTcNum := 10
   if len(os.Args) == 2 {
      aTcNum, err = strconv.Atoi(os.Args[1])
      if err != nil || aTcNum < 2 || aTcNum > 1000 {
         fmt.Fprintf(os.Stderr, "testclient count must be 2-1000\n")
         return 1
      }
   } else {
      err = sConfig.load()
      if err != nil {
         if !os.IsNotExist(err) {
            fmt.Fprintf(os.Stderr, "config load: %s\n", err.Error())
            return 1
         }
      } else {
         aTcNum = 0
      }
   }

   fmt.Printf("mnm tmtp server v%d.%d.%d %s\n", kVersionA, kVersionB, kVersionC, kVersionDate)

   aDbName := "userdb"; if aTcNum != 0 { aDbName += "-test-qlib" }
   qlib.UDb, err = NewUserDb(aDbName)
   if err != nil {
      fmt.Fprintf(os.Stderr, "%s\n", err.Error())
      return 1
   }

   qlib.Init("qstore")

   if aTcNum != 0 {
      fmt.Printf("Starting Test Pass\n")
      TestUserDb("userdb-test-unit")
      qlib.LocalTest(aTcNum)
   } else {
      err = startServer(&sConfig)
      if err != nil {
         fmt.Fprintf(os.Stderr, "server exit: %s\n", err.Error())
         return 1
      }
   }

   return 0
}

type tConfig struct {
   Listen struct {
      Net string
      Laddr string
      CertPath, KeyPath string
   }
}

func (o *tConfig) load() error {
   aBuf, err := ioutil.ReadFile(kConfigFile)
   if err != nil { return err }
   err = json.Unmarshal(aBuf, o)
   return err
}

func startServer(iConf *tConfig) error {
   var err error
   aCert, err := tls.LoadX509KeyPair(iConf.Listen.CertPath, iConf.Listen.KeyPath)
   if err != nil { return err }
   aCfgTls := tls.Config{Certificates: []tls.Certificate{aCert}}
   aListener, err := tls.Listen(iConf.Listen.Net, iConf.Listen.Laddr, &aCfgTls)
   if err != nil { return err }
   defer aListener.Close()

   for {
      var aConn net.Conn
      aConn, err = aListener.Accept()
      if err != nil { return err }
      qlib.NewLink(aConn)
   }
}
