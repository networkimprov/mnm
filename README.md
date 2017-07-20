## mnm

_Mnm is Not Mail_

But it is similar to e-mail! See [the Rationale](Rationale.md).

mnm is to be a general purpose message relay server, 
implementing an original protocol, Modern Messaging Transfer Protocol (MMTP). 

mnm provides:
- Reliable message storage (via fsync) and delivery (via ack)
- Message storage only until all recipients have ack'd receipt
- In-order message delivery from any given sender
- Distribution groups, with invitations and blockable members
- Unlimited aliases per user (including single-use aliases)
- Multiple clients/devices per user
- Per-client strong (200 bit) passwords

mnm does not provide:
- Message encryption; clients are responsible for encryption before/after transmission

mnm may provide:
- a gateway to whitelisted mnm & SMTP sites

mnm shall be accessible via several network frontends:
- TCP server
- HTTP server (separate receiver connection per client, as needed)
- HTTP + Websockets
- Unix domain sockets
- Arbitrary Golang frontend invoking qlib package

Written in Go (which compiles to an executable), mnm is intended to be
lightweight, fast, and dependency-free.

The author previously prototyped this in Node.js.
(Based on that experience, he can't recommend Node.js.)
_Warning, unreadable Javascript hackery follows._
http://github.com/networkimprov/websocket.MQ

### What's here

- qlib/qlib.go: package with simple API to the reciever & sender threads.
- qlib/testclient.go: in-process test client, invoked from main().
- mnm.go: main(), frontends (in progress), temporary home of tUserDb.
- vendor/qlib: symlink to qlib/ to simplify build
- After build & run:  
mnm: the app!  
userdb/: user & group data  
qstore/: queued messages awaiting delivery

### Quick start

1. go get github.com/networkimprov/mnm

2. go run mnm #currently starts test sequence  
_todo: prompt for key (or --key option) to decrypt userdb directory_

### Modern Messaging Transfer Protocol

MMTP defines a simple client/server exchange scheme; 
it needs no other protocol in the way that POP & IMAP need SMTP. 
MMTP may be conveyed by any reliable transport protocol, e.g. TCP, 
or tunneled through another protocol, e.g. HTTP. 
MMTP clients may simultaneously contact multiple MMTP servers. 
After the client completes a login or register request, either side may contact the other.

0. Headers precede every message  
`001f{ ... <,"dataLen":uint> }dataLen 8-bit bytes of data`  
Four hex digits give the size of the following JSON metadata,
which may be followed by arbitrary format 8-bit data.
Headers shall be encrypted with public keys for transmission.

_todo: protocol version request/response_

1. Register creates a user and client queue  
_todo: receive-only accounts which cannot ping or post_  
_todo: integrate with third party authentication services_  
`{"op":1, "newnode":string <,"newalias":string>}`  
.newnode is a reference to 1st client device  
Response _same as Login_  
At node `{"op":"registered", "uid":string, "nodeid":string <,"error":string>}`

2. UserEdit updates a user (in progress)  
_todo: dropnode and dropalias; prevent account hijacking from stolen client/nodeid_  
`{"op":2, "uid":string, "nodeid":string <,"newnode":string &| ,"newalias":string>}`  
.newnode is a reference to Nth client device  
Response `{"op":"updated" <,"nodeid":string>, "ok":"ok|error" <,"error":string>}`  
At nodes `{"op":"account", "id":string, "from":string <,"newnode":string &| ,"newalias"string>}`

3. Login connects a client to its queue  
_todo: notify other nodes_  
`{"op":3, "uid":string, "node":string}`  
Response `{"op":"info|quit" "info":string}` (also given on login timeout)  
? At nodes `{"op":"login", "id":string, "from":string, "info":string}`

4. GroupEdit creates or updates a group  
`in progress`

5. Post sends a message to users and/or groups  
_todo: return undelivered messages after N hours_  
`{"op":5, "id":string, "for":[{"id":string, "type":uint}, ...]}`  
.for[i].type: 1) user_id, 2) group_id (include self) 3) group_id (exclude self)  
Response `{"op":"ack", "id":string, "ok":"ok|error" <,"error":string>}`  
At recipient `{"op":"delivery", "id":string, "from":string}`

6. Ping sends a short text message via a user's alias.
A reply establishes contact between the parties.  
_todo: limit number of pings per user per 24h_  
`{"op":6, "id":string, "from":string, "to":string}`  
.from & .to are user aliases  
Response `{"op":"ack", "id":string, "ok":"ok|error" <,"error":string>}`  
At recipient `{"op":"ping", "id":string, "from":string}`

7. Ohi notifies chat contacts of presence (in progress)  
`{"op":n, "id":string, "for":[{"id":string}, ...]}`  
Response `{"op":"ack", "id":string, "ok":"ok|error" <,"error":string>}`  
At recipient `{"op":"ohi", "id":string, "from":string}`

7. Ack acknowledges receipt of a message  
`{"op":7, "id":string, "type":string}`

8. Quit performs logout  
`{"op":8}`

### Log

_23 June 2017_ -
Login, Post, Ack messages defined and handled.
qlib receiver (Link) and sender (tQueue) threads running,
 inter-linked by elastic msg id & net.Conn & ack channels.
Message storage in filesystem.
UserDatabase interface and storage functions drafted.
In-process client (tTestClient) exercising system.
Todo-next: ping, tUserDb implementation, free idle queues, stream long messages to/from storage.
