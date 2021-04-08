//+build js

package main

//go:generate cp /usr/local/go/misc/wasm/wasm_exec.js .

// compile with
// GOOS=js GOARCH=wasm go build -o main.wasm

//
// index.html
//
// <html>
// <head>
//     <meta charset="utf-8">
//     <script src="wasm_exec.js"></script>
//     <script>
//     const go = new Go();
//     WebAssembly.instantiateStreaming(fetch("main.wasm"), go.importObject).then((result) => {
//         go.run(result.instance);
//     });
//     </script>
// </head>
// <body></body>
// </html>

// to run
//
// bob = pakeInit("pass1","0");
// jane = pakeInit("pass1","1");
// jane = pakeUpdate(jane,pakePublic(bob));
// bob = pakeUpdate(bob,pakePublic(jane));
// jane = pakeUpdate(jane,pakePublic(bob));
// console.log(pakeSessionKey(bob))
// console.log(pakeSessionKey(jane))

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	"github.com/schollz/pake/v2"
	"golang.org/x/crypto/pbkdf2"
)

// ENCRYPTION

type Encryption struct {
	key        []byte
	passphrase []byte
	salt       []byte
}

// New generates a new Encryption, using the supplied passphrase and
// an optional supplied salt.
// Passing nil passphrase will not use decryption.
func NewEncryption(passphrase []byte, salt []byte) (e Encryption, err error) {
	if passphrase == nil {
		e = Encryption{nil, nil, nil}
		return
	}
	e.passphrase = passphrase
	if salt == nil {
		e.salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		rand.Read(e.salt)
	} else {
		e.salt = salt
	}
	e.key = pbkdf2.Key([]byte(passphrase), e.salt, 100, 32, sha256.New)
	return
}

func (e Encryption) Salt() []byte {
	return e.salt
}

// Encrypt will generate an Encryption, prefixed with the IV
func (e Encryption) Encrypt(plaintext []byte) (encrypted []byte, err error) {
	if e.passphrase == nil {
		encrypted = plaintext
		return
	}
	// generate a random iv each time
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	ivBytes := make([]byte, 12)
	rand.Read(ivBytes)
	b, err := aes.NewCipher(e.key)
	if err != nil {
		return
	}
	aesgcm, err := cipher.NewGCM(b)
	if err != nil {
		return
	}
	encrypted = aesgcm.Seal(nil, ivBytes, plaintext, nil)
	encrypted = append(ivBytes, encrypted...)
	return
}

// Decrypt an Encryption
func (e Encryption) Decrypt(encrypted []byte) (plaintext []byte, err error) {
	if e.passphrase == nil {
		plaintext = encrypted
		return
	}
	b, err := aes.NewCipher(e.key)
	if err != nil {
		return
	}
	aesgcm, err := cipher.NewGCM(b)
	if err != nil {
		return
	}
	plaintext, err = aesgcm.Open(nil, encrypted[:12], encrypted[12:], nil)
	return
}

// encrypt(message,password,salt)
func encrypt(this js.Value, inputs []js.Value) interface{} {
	if len(inputs) != 3 {
		return js.Global().Get("Error").New("not enough inputs")
	}
	e, err := NewEncryption([]byte(inputs[1].String()), []byte(inputs[2].String()))
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	enc, err := e.Encrypt([]byte(inputs[0].String()))
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return hex.EncodeToString(enc)
}

// decrypt(message,password,salt)
func decrypt(this js.Value, inputs []js.Value) interface{} {
	e, err := NewEncryption([]byte(inputs[1].String()), []byte(inputs[2].String()))
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	decBytes, err := hex.DecodeString(inputs[0].String())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	dec, err := e.Decrypt(decBytes)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return string(dec)
}

// initPake(weakPassphrase, role)
// returns: pakeBytes
func pakeInit(this js.Value, inputs []js.Value) interface{} {
	// initialize sender P ("0" indicates sender)
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need weakPassphrase, role")
	}
	role := 0
	if inputs[1].String() == "1" {
		role = 1
	}
	P, err := pake.Init([]byte(inputs[0].String()), role, elliptic.P521(), 1*time.Millisecond)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	bJSON, _ := json.Marshal(P)
	return string(bJSON)
}

// pakeUpdate(pakeBytes,otherPublicPakeBytes)
func pakeUpdate(this js.Value, inputs []js.Value) interface{} {
	if len(inputs) != 2 {
		return js.Global().Get("Error").New("need two input")
	}
	var P, Q *pake.Pake
	err := json.Unmarshal([]byte(inputs[0].String()), &P)
	P.SetCurve(elliptic.P521())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	err = json.Unmarshal([]byte(inputs[1].String()), &Q)
	Q.SetCurve(elliptic.P521())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	P.Update(Q.Bytes())
	bJSON, _ := json.Marshal(P)
	return string(bJSON)
}

// pakePublic(pakeBytes)
func pakePublic(this js.Value, inputs []js.Value) interface{} {
	var P *pake.Pake
	err := json.Unmarshal([]byte(inputs[0].String()), &P)
	P.SetCurve(elliptic.P521())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return string(P.Public().Bytes())
}

// pakeSessionKey(pakeBytes)
func pakeSessionKey(this js.Value, inputs []js.Value) interface{} {
	var P *pake.Pake
	err := json.Unmarshal([]byte(inputs[0].String()), &P)
	P.SetCurve(elliptic.P521())
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	key, err := P.SessionKey()
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return hex.EncodeToString(key)
}

func main() {
	c := make(chan bool)
	fmt.Println("starting")
	js.Global().Set("encrypt", js.FuncOf(encrypt))
	js.Global().Set("decrypt", js.FuncOf(decrypt))
	js.Global().Set("pakeInit", js.FuncOf(pakeInit))
	js.Global().Set("pakePublic", js.FuncOf(pakePublic))
	js.Global().Set("pakeUpdate", js.FuncOf(pakeUpdate))
	js.Global().Set("pakeSessionKey", js.FuncOf(pakeSessionKey))
	fmt.Println("Initiated")
	<-c
}
