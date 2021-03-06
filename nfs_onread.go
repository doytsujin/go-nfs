package nfs

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/willscott/go-nfs-client/nfs/xdr"
)

type nfsReadArgs struct {
	Handle []byte
	Offset uint64
	Count  uint32
}

type nfsReadResponse struct {
	Count uint32
	EOF   uint32
	Data  []byte
}

// MaxRead is the advertised largest buffer the server is willing to read
const MaxRead = 1 << 24

// CheckRead is a size where - if a request to read is larger than this,
// the server will stat the file to learn it's actual size before allocating
// a buffer to read into.
const CheckRead = 1 << 15

func onRead(ctx context.Context, w *response, userHandle Handler) error {
	w.errorFmt = opAttrErrorFormatter
	var obj nfsReadArgs
	err := xdr.Read(w.req.Body, &obj)
	if err != nil {
		// TODO: wrap
		return err
	}
	fs, path, err := userHandle.FromHandle(obj.Handle)
	if err != nil {
		return &NFSStatusError{NFSStatusStale}
	}

	fh, err := fs.Open(fs.Join(path...))
	if err != nil {
		// err
		return &NFSStatusError{NFSStatusAccess}
	}

	resp := nfsReadResponse{}

	if obj.Count > CheckRead {
		info, err := fs.Stat(fs.Join(path...))
		if err != nil {
			return &NFSStatusError{NFSStatusAccess}
		}
		if info.Size()-int64(obj.Offset) < int64(obj.Count) {
			obj.Count = uint32(uint64(info.Size()) - obj.Offset)
		}
	}
	if obj.Count > MaxRead {
		obj.Count = MaxRead
	}
	resp.Data = make([]byte, obj.Count)
	// todo: multiple reads if size isn't full
	cnt, err := fh.ReadAt(resp.Data, int64(obj.Offset))
	if err != nil && !errors.Is(err, io.EOF) {
		return &NFSStatusError{NFSStatusIO}
	}
	resp.Count = uint32(cnt)
	resp.Data = resp.Data[:resp.Count]
	if errors.Is(err, io.EOF) {
		resp.EOF = 1
	}

	writer := bytes.NewBuffer([]byte{})
	if err := xdr.Write(writer, uint32(NFSStatusOk)); err != nil {
		return err
	}
	WritePostOpAttrs(writer, tryStat(fs, path))

	if err := xdr.Write(writer, resp); err != nil {
		return err
	}
	return w.Write(writer.Bytes())
}
