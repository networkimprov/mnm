## mnm

_Mnm is Not Mail_

Nor is it a message queue. But it is similar to e-mail!

mnm is to be a general purpose message relay server. It provides:
- Reliable message storage (via fsync) and delivery (via ack)
- Message storage only until all recipients have ack'd receipt
- In-order message delivery from any given sender
- Distribution groups, with invitations and blockable members
- Unlimited aliases per user (including single-use aliases)
- Multiple clients/devices per user
- Per-client strong passwords

mnm does not provide:
- Message encryption; clients are responsible for encryption before/after transmission

mnm may provide:
- a gateway to whitelisted SMTP sites

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
- mnm.go: main(), frontends, temporary home of tUserDb.

### Protocol

0. Headers: 001f{ "op":X ... <,"dataLen":N> }N bytes of 8-bit data  
Four hex digits give the size of the following JSON metadata object,
which may be followed by arbitrary format 8-bit data.
Headers shall be encrypted with public keys for transmission.
1. Register
2. AddNode
3. Login: {"op":3, "uid":string, "node":string}   
Response {"op":"info|quit" "info":string} (also given on login timeout)
4. GroupEdit
5. Post: {"op":5, "id":string, "for":[{"id":string, "type":int}, ...]}  
.for[i].type: 0) single-node, 1) user_id, 2) group_id (include self) 3) group_id (exclude self)  
Response {"op":"ack", "id":string, "ok":"ok|error" <,"error":string>}
6. Ping
7. Ack: {"op":7, "id":string}

### Log

_23 June 2017_ -
Login, Post, Ack messages defined and handled.
qlib receiver (Link) and sender (tQueue) threads running,
 inter-linked by elastic msg id & net.Conn & ack channels.
Message storage in filesystem.
UserDatabase interface and storage functions drafted.
In-process client (tTestClient) exercising system.
Todo-next: ping, tUserDb implementation, free idle queues, stream long messages to/from storage.
