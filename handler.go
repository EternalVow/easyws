package easyws

type IEasyWs interface {
	OnStart() (OpCode, error)

	OnConnect() (OpCode, error)

	OnUpgraded() (OpCode, error)

	OnReceive(msg []byte) ([]byte, OpCode, error)

	OnShutdown() (OpCode, error)

	OnClose(err error) (OpCode, error)

	// todo to add more
}
