package main

import (
	"encoding/json"
	"testing"
)

func TestDecodeNativeStoreDropsLegacyProtocols(t *testing.T) {
	blob := []byte(`{
	  "next_id": 6,
	  "inbounds": [
	    {"id":1,"port":1001,"protocol":"vmess","network":"ws","security":"tls","clients":[{"id":"u","email":"e","enable":true}]},
	    {"id":2,"port":1002,"protocol":"trojan","network":"tcp","security":"tls","clients":[{"password":"p","email":"e","enable":true}]},
	    {"id":3,"port":1003,"protocol":"hysteria","network":"hysteria","security":"tls","clients":[{"password":"p","email":"e","enable":true}]},
	    {"id":4,"port":1004,"protocol":"vless","network":"hysteria","security":"tls","clients":[{"id":"u","email":"e","enable":true}]},
	    {"id":5,"port":1005,"protocol":"vless","network":"tcp","security":"none","clients":[{"id":"u","email":"e","enable":true}]}
	  ]
	}`)
	st, err := decodeNativeStore(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Inbounds) != 1 || st.Inbounds[0].ID != 5 || !st.droppedLegacy {
		t.Fatalf("legacy protocols were not filtered: %+v", st)
	}
	if _, err := json.Marshal(st); err != nil {
		t.Fatal(err)
	}
}
