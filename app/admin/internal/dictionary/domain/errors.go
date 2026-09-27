// Package domain contains the dictionary module's simple entities and port.
package domain

import "errors"

var (
	ErrDictTypeNotFound   = errors.New("domain: 字典类型不存在")
	ErrDictTypeDuplicated = errors.New("domain: 字典类型已存在")
	ErrDictDataNotFound   = errors.New("domain: 字典项不存在")
	ErrDictDataDuplicated = errors.New("domain: 同一字典下键值已存在")
)
