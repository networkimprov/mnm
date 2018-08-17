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
   "encoding/json"
   "net"
   "os"
   pQ "mnm/qlib"
   "os/signal"
   "strconv"
   "strings"
   "time"
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

   aTcNum := 0
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
         } else {
            fmt.Fprintf(os.Stderr, "config load: %s missing; see mnm.conf for example\n", kConfigFile)
         }
         return 1
      }
   }

   fmt.Printf("mnm tmtp server v%d.%d.%d %s\n", kVersionA, kVersionB, kVersionC, kVersionDate)

   aDbName := "userdb"; if aTcNum != 0 { aDbName += "-test-qlib" }
   pQ.UDb, err = NewUserDb(aDbName)
   if err != nil {
      fmt.Fprintf(os.Stderr, "%s\n", err.Error())
      return 1
   }

   pQ.Init("qstore")

   if aTcNum != 0 {
      fmt.Printf("Starting Test Pass\n")
      TestUserDb("userdb-test-unit")
      pQ.LocalTest(aTcNum)
   } else {
      err = startServer(&sConfig)
      if err != nil {
         fmt.Fprintf(os.Stderr, "server exit: %s\n", err.Error())
         return 1
      }
      fmt.Printf("server done\n")
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

   aIntWatch := make(chan os.Signal, 1)
   signal.Notify(aIntWatch, os.Interrupt)
   go func() {
      <-aIntWatch
      aListener.Close()
   }()

   var aConn net.Conn
   const kPauseMin, kPauseMax = time.Millisecond, time.Second
   aPause := kPauseMin
   for {
      aConn, err = aListener.Accept()
      if err != nil {
         if !err.(net.Error).Temporary() {
            pQ.Suspend()
            if strings.Contains(err.Error(), "use of closed network connection") {
               return nil
            }
            return err
         }
         if aPause > kPauseMax {
            aPause = kPauseMax
            fmt.Fprintf(os.Stderr, "listener recurring error %s\n", err.Error())
         }
         time.Sleep(aPause)
         aPause *= 2
         continue
      }
      aPause = kPauseMin
      pQ.NewLink(aConn)
   }
}
