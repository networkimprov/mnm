### Why mnm & MMTP?

_The Internet Crime Wave_

The Internet has facilitated the greatest organized crime wave since Prohibition 
([USA, 1920-33](https://en.wikipedia.org/wiki/Prohibition_in_the_United_States)). Problematically, 
most cybercriminals and industrial spies are overseas, beyond the reach of US or Western 
law enforcement. Many are quietly supported by their national governments. 
For the foreseeable future, this crime wave will worsen.

One of the most devastating weapons in the cybercriminal's arsenal is an Internet application 
which millions of us willingly use every day: **e-mail**. It enables criminals to...

1. Send you messages that appear to be from people you trust
2. Send you any content on first contact, including:  
   a) links to hostile websites masquerading as trusted ones  
   b) executable programs, i.e. malware  
   c) attractively-designed disinformation and scam offers  
3. Send you unlimited messages without your consent
4. Steal all your past correspondence by discovering a simple password
5. Steal all your organization's correspondence by breaking into a single server

These vulnerabilities have forced the adoption of spam filters that inevitably bury legitimate 
messages, yet fail to block carefully crafted or targeted attacks. Spam filters may even help such 
attackers, as they create a false sense of security.

SMTP, the protocol at the root of these problems, 
originated at a time when the links between Internet sites were slow and intermittent, 
and the only people using the Internet were friendly researchers in academia and government. 
SMTP cannot cope with the 21st Century, and must be replaced.

_Safer Modern Messaging_

Some requirements for a messaging system that doesn't have the above failings...

Every organization, whether tiny or enormous, needs a **members-only messaging service** 
that cannot receive traffic from external or unapproved senders. 
Organizations that need to let certain members hear from the general public, 
or communicate with untrusted (perhaps anonymous) customers, 
would establish a separate service instance for that purpose. 
For a small organization, the cost of the messaging service should be negligible. 

To prevent destructive correspondence (in the case of an organization with non-restrictive membership) 
the service must prevent members with whom you are not acquainted from sending you arbitrary content. 

To prevent theft of correspondence (in the event of a compromised account or server) the messaging service 
must **store only messages that have not yet been delivered** or returned as undeliverable. 
Where archiving is required, the service should encrypt the traffic of designated accounts 
with a public key, and forward it to an archive service.

The messaging service must also support message distribution lists (aka groups) and instant messaging (aka chat), 
as these features are natural extensions of a messaging system. 

The messaging service must reliably deliver each message to **every one of your devices** that runs a messaging app, 
e.g. laptop, mobile phone, tablet, smartwatch. 
If one of your messaging devices is lost or stolen, you must be able to bar it from further access to your 
messaging accounts. A compromised device must not be able to hijack your accounts. 

Since messaging services are organization-specific, and you may participate in multiple organizations, 
the messaging app on your devices must connect to any number of messaging services. 
The app should present a **separate inbox for each service**, and adjust the look of inbox & message views 
(e.g. fonts, colors, background graphics) according to the service's skin settings, which you may revise. 

For sensitive business data which cannot transit a network unencrypted, the app 
should allow encryption prior to send and decryption on receipt, using public or shared keys.
The app should also allow encryption of your message history on local storage. 
The app should automatically backup your message history to secondary local storage when available, 
e.g. a flash drive or microSD card. 
When you setup an additional device for messaging, 
the app must transfer your message history to the device in a peer-to-peer manner. 
The app should run on virtually any computing device you own. 

