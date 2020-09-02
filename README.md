### mnm is not mail

<img width="300" hspace="0" align="right" src="https://user-images.githubusercontent.com/458838/65545951-535f6980-decb-11e9-8f46-6122198097b0.png">  

The mnm project is building a legitimate replacement for email: 
a server (see below), a [client](https://github.com/networkimprov/mnm-hammer), and 
a [simple protocol](https://github.com/networkimprov/mnm/blob/master/Protocol.md) (TMTP) between them.

For an introduction to the project, or to try it out, see 
[the mnm app](https://github.com/networkimprov/mnm-hammer/blob/master/README.md).

TMTP is a new client/server protocol for reliable store-and-forward message delivery. 
Unlike SMTP, it does not allow anyone, anywhere, claiming any identity to send you any content, 
any number of times. 
Further reading: [_Why TMTP?_](Rationale.md) 

Written in Go, the mnm TMTP server is reliable, fast, lightweight, and open source. 

### Server features

A TMTP relay service must provide:
- Members-only access
- Multiple aliases per member (including single-use aliases)
- Invitations with limited content, addressed to an alias
- Distribution groups, by invitation
- Opt-in presence notification
- In-order message delivery from any given sender
- Delivery to multiple messaging devices per member
- Per-device strong (200 bit) passwords
- Reliable message storage (via fsync) and delivery (via ack)
- Message storage only until all recipients' clients have ack'd receipt
- Long-lived client connections over TCP+TLS

It does not provide:
- Message encryption; clients are responsible for encryption before/after transmission

It may provide:
- Per-member block lists
- Member authorization for receive/send or receive-only
- Distribution groups, by subscription
- Gateways to whitelisted TMTP & SMTP sites
- Alternate connection schemes
  * HTTP + Websockets
  * Unix domain sockets
  * Custom protocol via plugin

### Protocol

"Trusted Messaging Transfer Protocol" defines a simple client/server exchange scheme; 
it needs no other protocol in the way that POP & IMAP need SMTP. 
TMTP may be conveyed by any reliable transport protocol, e.g. TCP, 
or tunneled through another protocol, e.g. HTTP. 
A client may simultaneously contact multiple TMTP servers via separate connections. 
After the client completes a login or register request, either side may contact the other.

See the [TMTP Protocol docs](Protocol.md).

### Status

_13 April 2019_ -
A private preview is now live! Contact the author if you'd like to try it.

_19 August 2018_ -
After testing with mnm client, made a handful of fixes. Changed license to MPL.

_25 September 2017_ -
A [client application](https://github.com/networkimprov/mnm-hammer) is in development.

_3 August 2017_ -
A simulation of 1000 concurrent active clients 
delivers 1 million messages totaling 6.7GB in 46 minutes. 
It uses ~200MB RAM, <10MB disk, and minimal CPU time. 
Each client runs a 19-step cycle that does login, then post for two recipients (15x) 
or for a group of 100 (2x) every 1-30s, then logout and idle for 1-30s. 

### What's here

- qlib/: TMTP package with simple API
- userdb.go: user records management
- main.go: main(), network frontend
- mnm.conf: site-specific parameters; rename to mnm.config to enable TCP server
- codestyle.txt: how to make Go source much more clear
- mnm: the server executable
- After first run:  
  userdb/: user & group data  
  qstore/: queued messages awaiting delivery

### Quick start

1. Download & Build  
a) `go get github.com/networkimprov/mnm`  
b) `cd $GOPATH/src/github.com/networkimprov/mnm`

1. Enable TCP+TLS with self-signed certificate  
a) `openssl ecparam -genkey -name secp384r1 -out server.key`  
b) `openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650`  
c) `cp mnm.conf mnm.config` # edit to revise ntp.hosts and adjust listen.laddr with "host:port"

   Note: On a public Internet host, port 443 will see a steady trickle of probe requests 
   (often with nefarious intent) which pollutes the mnm logs. 
   Choose a port above 1024 to avoid this. 

1. Run  
a) `./mnm` # default port 443 may require sudo; logs to stdout/stderr  
b) ctrl-C to stop  
or  
a) `./mnm >> logfile 2>&1 &` # run in background, logs to end of logfile  
b) `kill -s INT <background_pid>` # send SIGINT signal, triggering graceful shutdown

### Testing

Continuous test sequence with simulated clients  
a) `./mnm 10` # may be 2-1000  
b) ctrl-C to stop

### License

Copyright 2017, 2018 Liam Breck  
Published at https://github.com/networkimprov/mnm

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at http://mozilla.org/MPL/2.0/

