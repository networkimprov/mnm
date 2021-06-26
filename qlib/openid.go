// Copyright 2021 Liam Breck
// Published at https://github.com/networkimprov/mnm
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/

package qlib

import (
   "encoding/base64"
   "math/big"
   "encoding/binary"
   "bytes"
   "crypto"
   "fmt"
   "net/http"
   "encoding/json"
   "os"
   "crypto/rand"
   "crypto/rsa"
   "crypto/sha256"
   "strconv"
   "time"
)

var sOpenidCfg []tOpenidCfg

type tOpenidCfg struct {
   url, iss string
   aud string
   keys []tOpenidKey
}

type tOpenidKey struct {
   Kty, Alg, Use string
   Kid string
   E tBase64Int
   N *tBase64BigInt
}

type tBase64Int int
type tBase64BigInt big.Int

func (o *tBase64Int) UnmarshalJSON(iStr []byte) error {
   iStr, err := _decodeBase64Url(iStr[1:len(iStr)-1])
   if err != nil { return err }
   if len(iStr) == 3 {
      iStr = append(iStr, 0)
   }
   *o = tBase64Int(binary.LittleEndian.Uint32(iStr))
   return nil
}

func (o *tBase64BigInt) UnmarshalJSON(iStr []byte) error {
   iStr, err := _decodeBase64Url(iStr[1:len(iStr)-1])
   if err != nil { return err }
   (*big.Int)(o).SetBytes(iStr)
   return nil
}

func _decodeBase64Url(iStr []byte) ([]byte, error) {
   aLen, err := base64.RawURLEncoding.Decode(iStr, iStr)
   return iStr[:aLen], err
}

type tOpenidToken struct {
   Scope, Token_type string
   Expires_in uint
   Access_token, Id_token string
   Refresh_token string `json:",omitempty"`
}

type tOpenidHeader struct {
   Kid string
   Alg string
}

type tOpenidClaims struct {
   Ver int
   Sub, Aud, Idp string
   Iss string
   Iat, Exp, Auth_time tUnixTime
   Jti string
   Amr []string
   At_hash string
}

type tUnixTime struct { time.Time }

func (o *tUnixTime) UnmarshalJSON(iStr []byte) error {
   aN, err := strconv.ParseUint(string(iStr), 10, 64)
   if err != nil { return err }
   *o = tUnixTime{time.Unix(int64(aN), 0).UTC()}
   return nil
}

func clearConfigOpenid() { sOpenidCfg = nil }

func addConfigOpenid(iUrl string, iIss string, iAud string) {
   sOpenidCfg = append(sOpenidCfg, tOpenidCfg{url: iUrl, iss: iIss, aud: iAud})
}

func initOpenid() {
   for a := range sOpenidCfg {
      aResp, err := http.Get(sOpenidCfg[a].url)
      if err != nil {
         fmt.Fprintf(os.Stderr, "OpenID config: could not obtain %s: %v\n  restart to retry",
                                sOpenidCfg[a].url, err)
         continue
      }
      var aKeys struct { Keys []tOpenidKey }
      err = json.NewDecoder(aResp.Body).Decode(&aKeys)
      aResp.Body.Close()
      if err != nil {
         fmt.Fprintf(os.Stderr, "OpenID config: could not parse response from %s: %v\n  restart to retry",
                                sOpenidCfg[a].url, err)
         continue
      }
      sOpenidCfg[a].keys = aKeys.Keys
   }
}

func validateTokenOpenid(iTok *tOpenidToken) (tMsg, error) {
   var err error
   aSet := bytes.Split([]byte(iTok.Id_token), []byte{'.'})
   if len(aSet) != 3 {
      return nil, tError("OpenID id_token string invalid")
   }

   aHash := sha256.New()
   aHash.Write(aSet[0])
   aHash.Write([]byte{'.'})
   aHash.Write(aSet[1])

   for a := range aSet {
      aSet[a], err = _decodeBase64Url(aSet[a])
      if err != nil {
         return nil, tError("OpenID id_token base64 invalid")
      }
   }

   var aHead tOpenidHeader
   err = json.Unmarshal(aSet[0], &aHead)
   if err != nil {
      return nil, tError("OpenID id_token JSON invalid")
   }
   var aClaims tOpenidClaims
   err = json.Unmarshal(aSet[1], &aClaims)
   if err != nil {
      return nil, tError("OpenID id_token JSON invalid")
   }

   if aClaims.Exp.Before(time.Now()) {
      return nil, tError("OpenID id_token expired")
   }

   var aCfg *tOpenidCfg
   for a := range sOpenidCfg {
      if sOpenidCfg[a].iss == aClaims.Iss && sOpenidCfg[a].aud == aClaims.Aud {
         aCfg = &sOpenidCfg[a]
         break
      }
   }
   if aCfg == nil {
      return nil, tError("OpenID id_token claims invalid")
   }

   var aPk *rsa.PublicKey
   for a := range aCfg.keys {
      if aCfg.keys[a].Kid == aHead.Kid && aCfg.keys[a].Alg == aHead.Alg {
         aPk = &rsa.PublicKey{N: (*big.Int)(aCfg.keys[a].N), E: int(aCfg.keys[a].E)}
         break
      }
   }
   if aPk == nil {
      return nil, tError("OpenID key not found; restart to reload keys")
   }

   err = rsa.VerifyPKCS1v15(aPk, crypto.SHA256, aHash.Sum(nil), aSet[2])
   if err != nil {
      return nil, tError("OpenID id_token signature invalid")
   }
   aMsg := tMsg{"Subject": aClaims.Sub, "Issuer": aClaims.Iss, "Audience": aClaims.Aud,
                "IssuedAt": aClaims.Iat.Format(time.RFC3339)}
   return aMsg, nil
}

func enableTestOpenid() *tOpenidToken {
   aPk, err := rsa.GenerateKey(rand.Reader, 1024)
   if err != nil { panic(err) }

   aKeys := struct { Keys []tMsg `json:"keys"` }{ []tMsg{{"alg":"RS256", "kid":"kid"}} }
   aBuf := make([]byte, 4)
   binary.LittleEndian.PutUint32(aBuf, uint32(aPk.E))
   if aBuf[3] == 0 { aBuf = aBuf[:3] }
   aKeys.Keys[0]["e"] = base64.RawURLEncoding.EncodeToString(aBuf)
   aKeys.Keys[0]["n"] = base64.RawURLEncoding.EncodeToString(aPk.N.Bytes())

   go func() {
      http.HandleFunc("/keys", func(cResp http.ResponseWriter, cReq *http.Request) {
         cResp.Header().Set("Content-Type", "application/json")
         err := json.NewEncoder(cResp).Encode(&aKeys)
         if err != nil {
            fmt.Fprintf(os.Stderr, "OpenID keys test: %v\n", err)
         }
      })
      err := http.ListenAndServe(":8080", nil) //todo enable command-line option for port
      if err != http.ErrServerClosed {
         fmt.Fprintf(os.Stderr, "OpenID keys test: %v\n", err)
      }
   }()

   aIdH := tMsg{"kid":"kid", "alg":"RS256"}
   aIdC := tMsg{"sub":"subject", "iss":"issuer", "aud":"audience",
                "exp": time.Now().Add(time.Minute).Unix()}
   aTok := tOpenidToken{Token_type:"Bearer", Expires_in:3600, Access_token:"access"}

   aBuf, err = json.Marshal(&aIdH)
   if err != nil { panic(err) }
   aTok.Id_token = base64.RawURLEncoding.EncodeToString(aBuf)

   aBuf, err = json.Marshal(&aIdC)
   if err != nil { panic(err) }
   aTok.Id_token += "." + base64.RawURLEncoding.EncodeToString(aBuf)

   aHash := sha256.New()
   aHash.Write([]byte(aTok.Id_token))
   aBuf, err = rsa.SignPKCS1v15(nil, aPk, crypto.SHA256, aHash.Sum(nil))
   if err != nil { panic(err) }
   aTok.Id_token += "." + base64.RawURLEncoding.EncodeToString(aBuf)

   return &aTok
}
