package security

import "runtime"

func WipeBytes(data []byte) {
	for index := range data {
		data[index] = 0
	}
	runtime.KeepAlive(data)
}
