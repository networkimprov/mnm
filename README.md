_Mnm is Not Mail_

mnm provides the benefits of email without the huge risks of allowing 
anyone, anywhere, claiming any identity to send you any content, any number of times. 

mnm also offers electronic correspondence features missing from traditional email, 
including forms/surveys which may be filled out and returned, 
charts via [Chart.js or Vega-Lite], hyperlinks to messages, and slide shows. 
It creates HTML-formatted messages via Markdown, which enables 
mouseless (i.e. rapid) composition of rich text with graphical elements. 

This codebase is for the **TMTP message relay server.** 
(See also the [mnm client](https://github.com/networkimprov/mnm-hammer).) 
Written in Go, the relay server is reliable, fast, lightweight, dependency-free, and open source.

TMTP is a new client/server protocol for person-to-person or machine-to-machine message delivery. 
See [Why TMTP?](Rationale.md)

A TMTP server provides:
- Members-only access
- Member aliases (including single-use aliases) to limit first-contact content
- Authorization for receive/send or receive-only
- Distribution groups, with invitations and member blocking
- Online presence notification
- Multiple messaging clients/devices per member
- Per-client strong (200 bit) passwords
- Reliable message storage (via fsync) and delivery (via ack)
- Message storage only until all recipients have ack'd receipt
- In-order message delivery from any given sender
- TCP + TLS connections

It does not provide:
- Message encryption; clients are responsible for encryption before/after transmission

It may provide:
- Gateways to whitelisted TMTP & SMTP sites
- Alternate connection schemes
  * HTTP + Websockets
  * Unix domain sockets
  * Your Go code calling qlib package

### Status

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

The author previously prototyped this in Node.js.
(Based on that experience, he can't recommend Node.js.)
_Warning, unreadable Javascript hackery follows._
http://github.com/networkimprov/websocket.MQ

### What's here

- qlib/qlib.go: TMTP package with simple API
- qlib/testclient.go: in-process test client, invoked from main()
- vendor/: [NTP](https://github.com/beevik/ntp) package (kudos to Brett Vickers)
- userdb.go: user records management
- main.go: main(), network frontend
- mnm.conf: site-specific parameters; rename to mnm.config to enable TCP server
- codestyle.txt: how to make Go source much more clear
- mnm: the server executable
- After first run:  
userdb/: user & group data  
qstore/: queued messages awaiting delivery

### Quick start

1. go get github.com/networkimprov/mnm

2. Start test sequence  
a) cd $GOPATH/src/github.com/networkimprov/mnm # or alternate directory for new files  
b) go run mnm 10 # run continuous test with simulated clients (may be 2-1000)  
c) ctrl-C to stop

3. Enable TCP+TLS (assumes above working directory)  
a) openssl ecparam -genkey -name secp384r1 -out server.key  
b) openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650  
c) cp mnm.conf mnm.config # revise ntp.hosts and adjust listen.laddr with host:port as necessary  
d) go run mnm # default port 443 may require sudo  
e) ctrl-C or send SIGINT signal to trigger graceful shutdown

### TMTP Summary

"Trusted Messaging Transfer Protocol" defines a simple client/server exchange scheme; 
it needs no other protocol in the way that POP & IMAP need SMTP. 
TMTP may be conveyed by any reliable transport protocol, e.g. TCP, 
or tunneled through another protocol, e.g. HTTP. 
A client may simultaneously contact multiple TMTP servers. 
After the client completes a login or register request, either side may contact the other.

Each message starts with a header, wherein four hex digits give the size of a JSON metadata object, 
which may be followed by arbitrary format 8-bit data: 
`001f{ ... <"dataLen":uint> }dataLen 8-bit bytes of data`

Protocol errors by the client and login failure/timeout cause the server to terminate the connection 
after emitting a quit response:  
`{"op":"quit", "error":string}`

Ack responses from the server have the following required headers:  
`"id":string, "msgid":string, "posted":string, <"error":string>`

Messages from the server have the following required message headers:  
`"id":string, "from":string, "posted":string, "headsum":uint`

Id strings generated by the server (ack.msgid & message.id) must be sequential, 
but need not be contiguous. An easy way to generate them is to contact an NTP 
(network time protocol) server on startup for the current time, calculate the number of 
nanoseconds since the epoch, and increment that value for each id string generated. 
This supports generation of up to 1B ids per second, and doesn't require a persistent 
record of the last id generated before shutdown.

0. TmtpRev gives the latest recognized protocol version; it must be the first message.  
`{"op":0, "id":"1"}`  
Response `{"op":"tmtprev", "id":"1"}`

0. Register creates a user and client queue.  
_todo: receive-only accounts which cannot ping or post_  
_todo: integrate with third party authentication services_  
`{"op":1, "newnode":string <,"newalias":string>}`  
.newnode is the user label for a client device  
Response _same as Login_  
At node `{"op":"registered", "uid":string, "nodeid":string <,"error":string>}`

0. Login connects a client to its queue.  
`{"op":2, "uid":string, "node":string}`  
Response `{"op":"info" "info":string, "ohi":[string,...]}`  
At nodes `{"op":"login", (message headers), "datalen":0, "node":string}`

0. UserEdit updates a user account.  
_todo: check & store label_  
_todo: dropnode and dropalias; prevent account hijacking from stolen client/nodeid_  
_todo: cork stops delivery to user's nodes, params date & for-list_  
`{"op":3, "id":string, <"newnode":string | "newalias":string>}`  
.newnode is the user label for a client device  
Response `{"op":"ack", (ack headers)}`  
At nodes `{"op":"user", (message headers), "datalen":0, <"nodeid":string | "newalias":string>}`

0. OhiEdit notifies selected contacts of a user's presence. 
On first request after login, no "ohiedit" message is sent to user's nodes.  
`{"op":4, "id":string, "for":[{"id":string}, ...], "type":"add|drop"}`  
Response `{"op":"ack", (ack headers)}`  
At nodes `{"op":"ohiedit", (message headers), datalen:0, "for":[{"id":string}, ...], "type":"add|drop"}`  
At recipient `{"op":"ohi", "from":string, "status":uint}`

0. GroupInvite invites someone to a group, creating it if necessary.  
`{"op":5, "id":string, "gid":string, "datalen":uint, <"datahead":uint>, <"datasum":uint>, "from":string, "to":string}`  
Response `{"op":"ack", (ack headers)}`  
At recipient `{"op":"invite", (message headers), "datalen":uint, <"datahead":uint>, <"datasum":uint>, "gid":string, "to":string}`  
At members `{"op":"member", (message headers), "datalen":0, "act":string, "gid":string, "alias":string, <"newalias":string>}`

0. GroupEdit updates a group.  
_todo: moderated group_  
_todo: closed group publishes aliases to moderators_  
`{"op":6, "id":string, "act":"join" , "gid":string, <"newalias":string>}`  
`{"op":6, "id":string, "act":"alias", "gid":string, "newalias":string}`  
`{"op":6, "id":string, "act":"drop" , "gid":string, "to":string}`  
Response `{"op":"ack", (ack headers)}`  
At members `{"op":"member", (message headers), "datalen":0, "act":string, "gid":string, "alias":string, <"newalias":string>}`

0. Post sends a message to users and/or groups.  
`{"op":7, "id":string, "datalen":uint, <"datahead":uint>, <"datasum":uint>, "for":[<{"id":string, "type":uint}, ...>]}`  
.for[i].type: 1) user_id, 2) group_id (include self) 3) group_id (exclude self)  
.for[]: only nodes of self  
Response `{"op":"ack", (ack headers)}`  
At recipient `{"op":"delivery", (message headers), "datalen":uint, <"datahead":uint>, <"datasum":uint>}`

0. PostNotify sends a message to users/groups and a separate notification to a larger set of users/groups.  
`{"op":8, "id":string, "datalen":uint, <"datahead":uint>, <"datasum":uint>,`  
` "for":[{"id":string, "type":uint}, ...], <"fornotself":true>,`  
` "notelen":uint, <"notehead":uint>, <"notesum":uint>, <"notefor":[{"id":string, "type":uint}, ...]>}`  
.notelen segment follows the header and is sent to the .notefor & .for lists and nodes of self  
.datasum pertains to data following .notelen with length (.datalen - .notelen)  
.fornotself excludes nodes of self, which are otherwise implicit in .for list  
Response `{"op":"ack", (ack headers)}` (ack.msgid = delivery.id = notify.postid)  
At recipient `{"op":"delivery", (message headers), "datalen":uint, <"datahead":uint>, <"datasum":uint>, "notify":uint}`  
At notified `{"op":"notify", (message headers), "datalen":uint, <"datahead":uint>, <"datasum":uint>, "postid":string}`

0. Ping sends a short text message via a user's alias.
A reply establishes contact between the parties.  
_todo: limit number of pings per 24h and consecutive failed pings_  
`{"op":9, "id":string, "datalen":uint, <"datahead":uint>, <"datasum":uint>, "to":string}`  
Response `{"op":"ack", (ack headers)}`  
At recipient `{"op":"ping", (message headers), "datalen":uint, <"datahead":uint>, <"datasum":uint>, "to":string}`

0. Ack acknowledges receipt of a message.  
`{"op":10, "id":string, "type":string}`

0. Pulse resets the connection timeout.  
`{"op":11}`

0. Quit performs logout.  
`{"op":12}`

### License

Copyright 2017, 2018 Liam Breck  
Published at https://github.com/networkimprov/mnm

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at http://mozilla.org/MPL/2.0/

