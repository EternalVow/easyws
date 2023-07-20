package easyws

import (
	"bytes"
	"context"
	//"fmt"
	"io"
	"net/http"

	"github.com/EternalVow/easynet"
	_interface "github.com/EternalVow/easynet/interface"
	"github.com/EternalVow/easyws/httphead"
)

type NetHandler struct {
	IsUpgrade     map[string]bool
	EasyWsHandler IEasyWs
}

func (h NetHandler) OnStart(conn _interface.IConnection) error {
	_, err := h.EasyWsHandler.OnStart()
	return err
}

func (h NetHandler) OnConnect(conn _interface.IConnection) error {
	h.IsUpgrade[conn.RemoteAddr()] = false
	_, err := h.EasyWsHandler.OnConnect()
	return err
}

func (h NetHandler) OnReceive(conn _interface.IConnection, stream _interface.IInputStream) ([]byte, error) {

	// handover
	isUpgrade, ok := h.IsUpgrade[conn.RemoteAddr()]
	if (!ok) || (!isUpgrade) {
		upgrader := &Upgrader{}
		_, out, err := upgrader.Upgrade(stream)
		if err != nil {
			return nil, err
		}
		h.IsUpgrade[conn.RemoteAddr()] = true
		_, err = h.EasyWsHandler.OnUpgraded()
		if err != nil {
			return nil, err
		}
		return out, nil
	}

	var outFrame []byte
	header, err := ReadHeader(stream)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, header.Length)
	data := stream.Begin(nil)
	copy(payload, data[:header.Length])
	if int64(len(data)) > header.Length {
		stream.End(data[header.Length:])
	} else {
		stream.End(nil)
	}

	if header.Masked {
		Cipher(payload, header.Mask, 0)
	}

	// to do something
	wsOutForBiz, opCode, err := h.EasyWsHandler.OnReceive(payload)
	if err != nil {
		return nil, err
	}
	var f Frame
	switch opCode {
	case OpText:
		f = NewTextFrame(wsOutForBiz)
	case OpBinary:
		f = NewBinaryFrame(wsOutForBiz)
	case OpPing:
		f = NewPongFrame(wsOutForBiz)
	case OpPong:
		f = NewPingFrame(wsOutForBiz)
	case OpClose:
		f = NewCloseFrame(wsOutForBiz)
	}

	// Reset the Masked flag, server frames must not be masked as
	// RFC6455 says.
	f.Header.Masked = false

	wheaderBuf, err := WriteHeader(f.Header)
	if err != nil {
		return nil, err
	}
	outFrame = make([]byte, len(wheaderBuf)+len(f.Payload))
	copy(outFrame[:len(wheaderBuf)], wheaderBuf)
	copy(outFrame[len(wheaderBuf):len(wheaderBuf)+len(f.Payload)], f.Payload)
	if f.Header.OpCode == OpClose {
		return nil, nil
	}
	return outFrame, nil

}

func (h NetHandler) OnShutdown(conn _interface.IConnection) error {
	_, err := h.EasyWsHandler.OnShutdown()
	return err
}

func (h NetHandler) OnClose(conn _interface.IConnection, err error) error {
	_, err = h.EasyWsHandler.OnClose(err)
	return err
}

func NewEasyWs(easyWsHanler IEasyWs, ip string, port int32) *EasyWs {
	config := easynet.NewDefaultNetConfig("tcp", ip, port)
	handler := &NetHandler{
		IsUpgrade:     map[string]bool{},
		EasyWsHandler: easyWsHanler,
	}
	net := easynet.NewEasyNet(context.Background(), "NetPoll", config, handler)
	ws := &EasyWs{
		EasyNetHandler: handler,
		EasyNet:        net,
		EasyWsHandler:  easyWsHanler,
	}

	return ws
}

type EasyWs struct {
	EasyNetHandler *NetHandler

	EasyNet *easynet.EasyNet

	EasyWsHandler IEasyWs
}

// Upgrader contains options for upgrading connection to websocket.
type Upgrader struct {
	// ReadBufferSize and WriteBufferSize is an I/O buffer sizes.
	// They used to read and write http data while upgrading to WebSocket.
	// Allocated buffers are pooled with sync.Pool to avoid extra allocations.
	//
	// If a size is zero then default value is used.
	//
	// Usually it is useful to set read buffer size bigger than write buffer
	// size because incoming request could contain long header values, such as
	// Cookie. Response, in other way, could be big only if user write multiple
	// custom headers. Usually response takes less than 256 bytes.
	ReadBufferSize, WriteBufferSize int

	// Protocol is a select function that is used to select subprotocol
	// from list requested by client. If this field is set, then the first matched
	// protocol is sent to a client as negotiated.
	//
	// The argument is only valid until the callback returns.
	Protocol func([]byte) bool

	// ProtocolCustrom allow user to parse Sec-WebSocket-Protocol header manually.
	// Note that returned bytes must be valid until Upgrade returns.
	// If ProtocolCustom is set, it used instead of Protocol function.
	ProtocolCustom func([]byte) (string, bool)

	// Extension is a select function that is used to select extensions
	// from list requested by client. If this field is set, then the all matched
	// extensions are sent to a client as negotiated.
	//
	// Note that Extension may be called multiple times and implementations
	// must track uniqueness of accepted extensions manually.
	//
	// The argument is only valid until the callback returns.
	//
	// According to the RFC6455 order of extensions passed by a client is
	// significant. That is, returning true from this function means that no
	// other extension with the same name should be checked because server
	// accepted the most preferable extension right now:
	// "Note that the order of extensions is significant.  Any interactions between
	// multiple extensions MAY be defined in the documents defining the extensions.
	// In the absence of such definitions, the interpretation is that the header
	// fields listed by the client in its request represent a preference of the
	// header fields it wishes to use, with the first options listed being most
	// preferable."
	//
	// Deprecated: use Negotiate instead.
	Extension func(httphead.Option) bool

	// ExtensionCustom allow user to parse Sec-WebSocket-Extensions header
	// manually.
	//
	// If ExtensionCustom() decides to accept received extension, it must
	// append appropriate option to the given slice of Option.
	// It returns results of append() to the given slice and a flag that
	// reports whether given header value is wellformed or not.
	//
	// Note that ExtensionCustom may be called multiple times and
	// implementations must track uniqueness of accepted extensions manually.
	//
	// Note that returned options should be valid until Upgrade returns.
	// If ExtensionCustom is set, it used instead of Extension function.
	ExtensionCustom func([]byte, []httphead.Option) ([]httphead.Option, bool)

	// Negotiate is the callback that is used to negotiate extensions from
	// the client's offer. If this field is set, then the returned non-zero
	// extensions are sent to the client as accepted extensions in the
	// response.
	//
	// The argument is only valid until the Negotiate callback returns.
	//
	// If returned error is non-nil then connection is rejected and response is
	// sent with appropriate HTTP error code and body set to error message.
	//
	// RejectConnectionError could be used to get more control on response.
	Negotiate func(httphead.Option) (httphead.Option, error)

	// Header is an optional HandshakeHeader instance that could be used to
	// write additional headers to the handshake response.
	//
	// It used instead of any key-value mappings to avoid allocations in user
	// land.
	//
	// Note that if present, it will be written in any result of handshake.
	Header HandshakeHeader

	// OnRequest is a callback that will be called after request line
	// successful parsing.
	//
	// The arguments are only valid until the callback returns.
	//
	// If returned error is non-nil then connection is rejected and response is
	// sent with appropriate HTTP error code and body set to error message.
	//
	// RejectConnectionError could be used to get more control on response.
	OnRequest func(uri []byte) error

	// OnHost is a callback that will be called after "Host" header successful
	// parsing.
	//
	// It is separated from OnHeader callback because the Host header must be
	// present in each request since HTTP/1.1. Thus Host header is non-optional
	// and required for every WebSocket handshake.
	//
	// The arguments are only valid until the callback returns.
	//
	// If returned error is non-nil then connection is rejected and response is
	// sent with appropriate HTTP error code and body set to error message.
	//
	// RejectConnectionError could be used to get more control on response.
	OnHost func(host []byte) error

	// OnHeader is a callback that will be called after successful parsing of
	// header, that is not used during WebSocket handshake procedure. That is,
	// it will be called with non-websocket headers, which could be relevant
	// for application-level logic.
	//
	// The arguments are only valid until the callback returns.
	//
	// If returned error is non-nil then connection is rejected and response is
	// sent with appropriate HTTP error code and body set to error message.
	//
	// RejectConnectionError could be used to get more control on response.
	OnHeader func(key, value []byte) error

	// OnBeforeUpgrade is a callback that will be called before sending
	// successful upgrade response.
	//
	// Setting OnBeforeUpgrade allows user to make final application-level
	// checks and decide whether this connection is allowed to successfully
	// upgrade to WebSocket.
	//
	// It must return non-nil either HandshakeHeader or error and never both.
	//
	// If returned error is non-nil then connection is rejected and response is
	// sent with appropriate HTTP error code and body set to error message.
	//
	// RejectConnectionError could be used to get more control on response.
	OnBeforeUpgrade func() (header HandshakeHeader, err error)
}

// Upgrade zero-copy upgrades connection to WebSocket. It interprets given conn
// as connection with incoming HTTP Upgrade request.
//
// It is a caller responsibility to manage i/o timeouts on conn.
//
// Non-nil error means that request for the WebSocket upgrade is invalid or
// malformed and usually connection should be closed.
// Even when error is non-nil Upgrade will write appropriate response into
// connection in compliance with RFC.
func (u Upgrader) Upgrade(stream _interface.IInputStream) (hs Handshake, out []byte, err error) {
	// headerSeen constants helps to report whether or not some header was seen
	// during reading request bytes.
	const (
		headerSeenHost = 1 << iota
		headerSeenUpgrade
		headerSeenConnection
		headerSeenSecVersion
		headerSeenSecKey

		// headerSeenAll is the value that we expect to receive at the end of
		// headers read/parse loop.
		headerSeenAll = 0 |
			headerSeenHost |
			headerSeenUpgrade |
			headerSeenConnection |
			headerSeenSecVersion |
			headerSeenSecKey
	)

	// Read HTTP request line like "GET /ws HTTP/1.1".
	rl, err := readLine(stream)
	if err != nil {
		return hs, nil, err
	}
	// Parse request line data like HTTP version, uri and method.
	req, err := httpParseRequestLine(rl)
	if err != nil {
		return hs, nil, err
	}

	// Prepare stack-based handshake header list.
	header := handshakeHeader{
		0: u.Header,
	}

	// Parse and check HTTP request.
	// As RFC6455 says:
	//   The client's opening handshake consists of the following parts. If the
	//   server, while reading the handshake, finds that the client did not
	//   send a handshake that matches the description below (note that as per
	//   [RFC2616], the order of the header fields is not important), including
	//   but not limited to any violations of the ABNF grammar specified for
	//   the components of the handshake, the server MUST stop processing the
	//   client's handshake and return an HTTP response with an appropriate
	//   error code (such as 400 Bad Request).
	//
	// See https://tools.ietf.org/html/rfc6455#section-4.2.1

	// An HTTP/1.1 or higher GET request, including a "Request-URI".
	//
	// Even if RFC says "1.1 or higher" without mentioning the part of the
	// version, we apply it only to minor part.
	switch {
	case req.major != 1 || req.minor < 1:
		// Abort processing the whole request because we do not even know how
		// to actually parse it.
		err = ErrHandshakeBadProtocol

	case httphead.BtsToString(req.method) != http.MethodGet:
		err = ErrHandshakeBadMethod

	default:
		if onRequest := u.OnRequest; onRequest != nil {
			err = onRequest(req.uri)
		}
	}
	// Start headers read/parse loop.
	var (
		// headerSeen reports which header was seen by setting corresponding
		// bit on.
		headerSeen byte

		nonce = make([]byte, nonceSize)
	)
	for err == nil {
		line, e := readLine(stream)
		if e != nil {
			return hs, nil, e
		}
		if len(httphead.Trim(string(line))) == 0 {
			// Blank line, no more lines to read.
			break
		}

		k, v, ok := httpParseHeaderLine(line)
		if !ok {
			err = ErrMalformedRequest
			break
		}

		switch httphead.BtsToString(k) {
		case headerHostCanonical:
			headerSeen |= headerSeenHost
			if onHost := u.OnHost; onHost != nil {
				err = onHost(v)
			}

		case headerUpgradeCanonical:
			headerSeen |= headerSeenUpgrade
			if !bytes.Equal(v, specHeaderValueUpgrade) && !bytes.EqualFold(v, specHeaderValueUpgrade) {
				err = ErrHandshakeBadUpgrade
			}

		case headerConnectionCanonical:
			headerSeen |= headerSeenConnection
			if !bytes.Equal(v, specHeaderValueConnection) /*&& !btsHasToken(v, specHeaderValueConnectionLower)*/ {
				err = ErrHandshakeBadConnection
			}

		case headerSecVersionCanonical:
			headerSeen |= headerSeenSecVersion
			if !bytes.Equal(v, specHeaderValueSecVersion) {
				err = ErrHandshakeUpgradeRequired
			}

		case headerSecKeyCanonical:
			headerSeen |= headerSeenSecKey
			if len(v) != nonceSize {
				err = ErrHandshakeBadSecKey
			} else {
				copy(nonce, v)
			}

		case headerSecProtocolCanonical:
			if custom, check := u.ProtocolCustom, u.Protocol; hs.Protocol == "" && (custom != nil || check != nil) {
				var ok bool
				if custom != nil {
					hs.Protocol, ok = custom(v)
				} else {
					hs.Protocol, ok = btsSelectProtocol(v, check)
				}
				if !ok {
					err = ErrMalformedRequest
				}
			}

		case headerSecExtensionsCanonical:
			if f := u.Negotiate; err == nil && f != nil {
				hs.Extensions, err = negotiateExtensions(v, hs.Extensions, f)
			}
			// DEPRECATED path.
			if custom, check := u.ExtensionCustom, u.Extension; u.Negotiate == nil && (custom != nil || check != nil) {
				var ok bool
				if custom != nil {
					hs.Extensions, ok = custom(v, hs.Extensions)
				} else {
					hs.Extensions, ok = btsSelectExtensions(v, hs.Extensions, check)
				}
				if !ok {
					err = ErrMalformedRequest
				}
			}

		default:
			if onHeader := u.OnHeader; onHeader != nil {
				err = onHeader(k, v)
			}
		}
	}
	var buffer bytes.Buffer
	switch {
	case err == nil && headerSeen != headerSeenAll:
		switch {
		case headerSeen&headerSeenHost == 0:
			// As RFC2616 says:
			//   A client MUST include a Host header field in all HTTP/1.1
			//   request messages. If the requested URI does not include an
			//   Internet host name for the service being requested, then the
			//   Host header field MUST be given with an empty value. An
			//   HTTP/1.1 proxy MUST ensure that any request message it
			//   forwards does contain an appropriate Host header field that
			//   identifies the service being requested by the proxy. All
			//   Internet-based HTTP/1.1 servers MUST respond with a 400 (Bad
			//   Request) status code to any HTTP/1.1 request message which
			//   lacks a Host header field.
			err = ErrHandshakeBadHost
		case headerSeen&headerSeenUpgrade == 0:
			err = ErrHandshakeBadUpgrade
		case headerSeen&headerSeenConnection == 0:
			err = ErrHandshakeBadConnection
		case headerSeen&headerSeenSecVersion == 0:
			// In case of empty or not present version we do not send 426 status,
			// because it does not meet the ABNF rules of RFC6455:
			//
			// version = DIGIT | (NZDIGIT DIGIT) |
			// ("1" DIGIT DIGIT) | ("2" DIGIT DIGIT)
			// ; Limited to 0-255 range, with no leading zeros
			//
			// That is, if version is really invalid – we sent 426 status as above, if it
			// not present – it is 400.
			err = ErrHandshakeBadSecVersion
		case headerSeen&headerSeenSecKey == 0:
			err = ErrHandshakeBadSecKey
		default:
			panic("unknown headers state")
		}

	case err == nil && u.OnBeforeUpgrade != nil:
		header[1], err = u.OnBeforeUpgrade()
	}
	if err != nil {
		var code int
		if rej, ok := err.(*ConnectionRejectedError); ok {
			code = rej.code
			header[1] = rej.header
		}
		if code == 0 {
			code = http.StatusInternalServerError
		}
		httpWriteResponseError(&buffer, err, code, header.WriteTo)
		// Do not store Flush() error to not override already existing one.
		return hs, []byte(buffer.String()), err
	}

	httpWriteResponseUpgrade(&buffer, nonce, hs, header.WriteTo)

	return hs, []byte(buffer.String()), err
}

type handshakeHeader [2]HandshakeHeader

func (hs handshakeHeader) WriteTo(w io.Writer) (n int64, err error) {
	for i := 0; i < len(hs) && err == nil; i++ {
		if h := hs[i]; h != nil {
			var m int64
			m, err = h.WriteTo(w)
			n += m
		}
	}
	return n, err
}
