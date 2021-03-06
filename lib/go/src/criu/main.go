package criu

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/golang/protobuf/proto"
	"github.com/checkpoint-restore/criu/lib/go/src/rpc"
	"runtime"
)

type Criu struct {
	swrk_cmd *exec.Cmd
	swrk_sk  *os.File
}

func MakeCriu() *Criu {
	return &Criu{}
}

func (c *Criu) Prepare() error {
	fds, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return err
	}

	cln := os.NewFile(uintptr(fds[0]), "criu-xprt-cln")
	syscall.CloseOnExec(fds[0])
	srv := os.NewFile(uintptr(fds[1]), "criu-xprt-srv")
	defer srv.Close()

	args := []string{"swrk", strconv.Itoa(fds[1])}
	cmd := exec.Command("criu", args...)

	err = cmd.Start()
	if err != nil {
		cln.Close()
		return err
	}

	c.swrk_cmd = cmd
	c.swrk_sk = cln

	return nil
}

func (c *Criu) Cleanup() {
	if c.swrk_cmd != nil {
		c.swrk_sk.Close()
		c.swrk_sk = nil
		c.swrk_cmd.Wait()
		c.swrk_cmd = nil
	}
}

func (c *Criu) sendAndRecv(req_b []byte) ([]byte, int, error) {
	cln := c.swrk_sk
	_, err := cln.Write(req_b)
	if err != nil {
		return nil, 0, err
	}

	resp_b := make([]byte, 2*4096)
	n, err := cln.Read(resp_b)
	if err != nil {
		return nil, 0, err
	}

	return resp_b, n, nil
}

func (c *Criu) doSwrk(req_type rpc.CriuReqType, opts *rpc.CriuOpts, nfy CriuNotify) error {
	resp, err := c.doSwrkWithResp(req_type, opts, nfy)
	if err != nil {
		return err
	}
	resp_type := resp.GetType()
	if resp_type != req_type {
		return errors.New("unexpected response")
	}

	return nil
}

func (c *Criu) doSwrkWithResp(req_type rpc.CriuReqType, opts *rpc.CriuOpts, nfy CriuNotify) (*rpc.CriuResp, error) {
	var resp *rpc.CriuResp

	req := rpc.CriuReq{
		Type: &req_type,
		Opts: opts,
	}

	if nfy != nil {
		opts.NotifyScripts = proto.Bool(true)
	}

	if c.swrk_cmd == nil {
		err := c.Prepare()
		if err != nil {
			return nil, err
		}

		defer c.Cleanup()
	}

	for {
		req_b, err := proto.Marshal(&req)
		if err != nil {
			return nil, err
		}

		fmt.Printf("sending a %s req! for pid %d\n. Here's the struct: %+v\n",req.Type.String(), req.Pid, req)
		resp_b, resp_s, err := c.sendAndRecv(req_b)
		if err != nil {
			return nil, err
		}

		resp = &rpc.CriuResp{}
		err = proto.Unmarshal(resp_b[:resp_s], resp)
		if err != nil {
			return nil, err
		}

		if !resp.GetSuccess() {
			return resp, fmt.Errorf("operation failed (msg:%s err:%d)",
				resp.GetCrErrmsg(), resp.GetCrErrno())
		}

		resp_type := resp.GetType()
		if resp_type != rpc.CriuReqType_NOTIFY {
			break
		}
		if nfy == nil {
			return resp, errors.New("unexpected notify")
		}

		notify := resp.GetNotify()
		switch notify.GetScript() {
		case "pre-dump":
			err = nfy.PreDump()
		case "post-dump":
			err = nfy.PostDump()
			fmt.Printf("Postdump result: %s\n", err)
			_, file, no, ok := runtime.Caller(1)
			if ok {
				fmt.Printf("called from %s#%d\n", file, no)
			}
		case "pre-restore":
			err = nfy.PreRestore()
		case "post-restore":
			err = nfy.PostRestore(notify.GetPid())
		case "network-lock":
			err = nfy.NetworkLock()
		case "network-unlock":
			err = nfy.NetworkUnlock()
		case "setup-namespaces":
			err = nfy.SetupNamespaces(notify.GetPid())
		case "post-setup-namespaces":
			err = nfy.PostSetupNamespaces()
		case "post-resume":
			err = nfy.PostResume()
		default:
			err = nil
		}

		if err != nil {
			return resp, err
		}

		req = rpc.CriuReq{
			Type:          &resp_type,
			NotifySuccess: proto.Bool(true),
		}
	}

	return resp, nil
}

func (c *Criu) Dump(opts rpc.CriuOpts, nfy CriuNotify) error {
	return c.doSwrk(rpc.CriuReqType_DUMP, &opts, nfy)
}

func (c *Criu) Restore(opts rpc.CriuOpts, nfy CriuNotify) error {
	return c.doSwrk(rpc.CriuReqType_RESTORE, &opts, nfy)
}

func (c *Criu) PreDump(opts rpc.CriuOpts, nfy CriuNotify) error {
	return c.doSwrk(rpc.CriuReqType_PRE_DUMP, &opts, nfy)
}

func (c *Criu) StartPageServer(opts rpc.CriuOpts) error {
	return c.doSwrk(rpc.CriuReqType_PAGE_SERVER, &opts, nil)
}

func (c *Criu) StartLazyPages(opts rpc.CriuOpts) error {
	fmt.Printf("Made it into StartLazyPages(opts)!\n")
	//resp, err := c.doSwrkWithResp(rpc.CriuReqType_LAZY_PAGES, &opts, nil)
	//fmt.Printf("Made it after doSwrkWithResp! resp: %d %d\n",resp.Ps.GetPid(), resp.Ps.GetPort())
	//if err != nil {
	//	fmt.Println(err)
	//	return 0, 0, err
	//}
	//fmt.Println("Returning from StartLazyPages(opts)")
	//return int(resp.Ps.GetPid()), int(resp.Ps.GetPort()), nil
	return c.doSwrk(rpc.CriuReqType_LAZY_PAGES, &opts, nil)
}

func (c *Criu) StartPageServerChld(opts rpc.CriuOpts) (int, int, error) {
	resp, err := c.doSwrkWithResp(rpc.CriuReqType_PAGE_SERVER_CHLD, &opts, nil)
	if err != nil {
		return 0, 0, err
	}

	return int(resp.Ps.GetPid()), int(resp.Ps.GetPort()), nil
}
