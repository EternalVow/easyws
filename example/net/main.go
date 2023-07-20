package main

import (
	"fmt"
	"github.com/EternalVow/easyws"
)

type handler struct {

}

func (h handler) OnStart() (easyws.OpCode, error) {
	return easyws.OpContinuation, nil
}

func (h handler) OnConnect() (easyws.OpCode, error) {
	return easyws.OpContinuation, nil
}

func (h handler) OnUpgraded() (easyws.OpCode, error) {
	return easyws.OpContinuation, nil
}

func (h handler) OnReceive(msg []byte) ([]byte, easyws.OpCode, error) {
	fmt.Println(string(msg))
	return msg,easyws.OpText, nil
}

func (h handler) OnShutdown() (easyws.OpCode, error) {
	return easyws.OpContinuation, nil
}

func (h handler) OnClose(err error) (easyws.OpCode, error) {
	return easyws.OpContinuation, nil
}

func main() {
	h:=handler{}
	ws := easyws.NewEasyWs(h,"127.0.0.1", 9001)
	fmt.Println(ws)

}
