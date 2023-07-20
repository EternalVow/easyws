package httphead

import "strings"

func StrToBytes(str string) (bts []byte) {
	return []byte(str)
}

func BtsToString(bts []byte) (str string) {
	return string(bts)
}

func Trim(s string) string {
	s = strings.Replace(s, "\n", "", -1)
	s = strings.Replace(s, "\r", "", -1)
	s = strings.Replace(s, " ", "", -1)
	return s
}
