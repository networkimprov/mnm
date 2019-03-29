### Why TMTP?

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
In desperation, organizations have turned to proprietary SaaS messaging products, 
which lock them into a closed network built for the convenience of the vendor, not its customers.

SMTP, the protocol at the root of these problems, 
originated at a time when the links between Internet sites were slow and intermittent, 
and the only people using the Internet were friendly researchers in academia and government. 
SMTP cannot cope with the 21st Century, and must be phased out.

References  
https://arstechnica.com/information-technology/2019/02/catastrophic-hack-on-email-provider-destroys-almost-two-decades-of-data/  
https://krebsonsecurity.com/2013/06/the-value-of-a-hacked-email-account/  
https://qz.com/1329961/hackers-account-for-90-of-login-attempts-at-online-retailers/  
https://www.wired.com/story/how-email-open-tracking-quietly-took-over-the-web/  
http://www.nytimes.com/2017/08/21/business/dealbook/phone-hack-bitcoin-virtual-currency.html  
http://www.zerohedge.com/news/2017-08-21/one-statistics-professor-was-just-banned-google-here-his-story  
http://edition.cnn.com/2017/07/31/politics/white-house-officials-tricked-by-email-prankster/  
https://www.wired.com/2012/08/apple-amazon-mat-honan-hacking/  

### Supplanting SMTP

_Requirements for a TMTP Network_

__A. Multiple Members-only Services:__

1. Every organization, whether tiny or enormous, needs a members-only messaging service 
that **cannot receive traffic from external or unapproved senders**. 

1. Organizations that need to let certain members hear from the general public, 
or communicate with untrusted (perhaps anonymous) customers, 
may establish a separate service instance for that purpose. 

1. To prevent destructive correspondence (in the case of organizations with non-restrictive membership) 
messaging services must **prevent members with whom you are not acquainted** 
from sending you arbitrary content. 

1. To prevent theft of correspondence (in the event of a compromised account or server) the messaging service 
must **store only messages that have not yet been delivered** or returned as undeliverable. 

1. Where archiving is required, the service should encrypt the traffic of designated accounts 
with a public key, and forward it to a write-only archive service.

1. The messaging service must support message distribution lists (aka groups) and online presence notification, 
as these are natural extensions of a messaging system. 

1. For a small organization, the cost of the messaging service should be negligible. 

__B. Single Client Application:__

1. Every individual needs a single messaging app that runs on virtually any computing device you own, 
and regularly **connects to the messaging services of all the organizations** you belong to, 
via a common network protocol. 

1. The app should present a **separate inbox for each service**, and adjust the look of inbox & message views 
(e.g. fonts, colors, background graphics) according to the service's skin settings, which you may revise. 

1. The messaging services must reliably **deliver every message to each of your devices** 
that runs the app. 

1. If one of those devices is lost or stolen, you must be able to bar it from further access to your 
messaging accounts. A compromised device must not be able to hijack your accounts. 

1. When you setup an additional device for messaging, 
the app must transfer your message history to the device in a peer-to-peer manner. 

1. The app should provide a peer-to-peer method to let people in face-to-face contact exchange invitations 
to messaging services. 

1. The app should enable encryption of your message history on local storage, and 
automatically backup the history to secondary local storage when available, 
e.g. a flash drive or microSD card. 

1. For sensitive business data which must not transit a network unencrypted, the messaging app 
should allow encryption prior to send and decryption on receipt, using public or shared keys.
