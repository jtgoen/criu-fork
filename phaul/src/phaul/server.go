package phaul

import (
	"fmt"
	"os"

	"github.com/golang/protobuf/proto"
	"github.com/checkpoint-restore/criu/lib/go/src/criu"
	"github.com/checkpoint-restore/criu/lib/go/src/rpc"
	"path/filepath"
)

type PhaulServer struct {
	cfg     PhaulConfig
	imgs    *images
	cr      *criu.Criu
	process *os.Process
}

/*
 * Main entry point. Make the server with comm and call PhaulRemote
 * methods on it upon client requests.
 */
func MakePhaulServer(c PhaulConfig) (*PhaulServer, error) {
	img, err := preparePhaulImages(c.Wdir)
	if err != nil {
		return nil, err
	}

	cr := criu.MakeCriu()

	return &PhaulServer{imgs: img, cfg: c, cr: cr}, nil
}

/*
 * PhaulRemote methods
 */

func (s *PhaulServer) StartLazyPages() error {
	fmt.Printf("S: start lazy pages\n")
	psi := rpc.CriuPageServerInfo{
		Fd: proto.Int32(int32(s.cfg.Memfd)),
	}
	opts := rpc.CriuOpts{
		LogLevel: proto.Int32(4),
		LogFile:  proto.String("lp.log"),
		Ps:       &psi,
		LazyPages: proto.Bool(true),
	}

	img_dir, err := os.Open(s.imgs.dir)
	fmt.Printf("img_dir: %s\n", s.imgs.dir)
	if err != nil {
		return err
	}
	defer img_dir.Close()

	opts.ImagesDirFd = proto.Int32(int32(img_dir.Fd()))

	pid, _, err := s.cr.StartLazyPages(opts)
	if err != nil {
		return err
	}
	
	fmt.Println("Past s.cr.StartLazyPages(opts)")
	s.process, err = os.FindProcess(pid)
	fmt.Printf("s.process: %d \n",s.process.Pid)
	if err != nil {
		return err
	}
	
	fmt.Println("Returning from server StartLazyPages()")
	return nil
}

func (s *PhaulServer) StartIter() error {
	fmt.Printf("S: start iter\n")
	psi := rpc.CriuPageServerInfo{
		Fd: proto.Int32(int32(s.cfg.Memfd)),
	}
	opts := rpc.CriuOpts{
		LogLevel: proto.Int32(4),
		LogFile:  proto.String("ps.log"),
		Ps:       &psi,
	}

	prev_p := s.imgs.lastImagesDir()
	img_dir, err := s.imgs.openNextDir()
	if err != nil {
		return err
	}
	defer img_dir.Close()

	opts.ImagesDirFd = proto.Int32(int32(img_dir.Fd()))
	if prev_p != "" {
		p, err := filepath.Abs(img_dir.Name())
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p, prev_p)
		if err != nil {
			return err
		}
		opts.ParentImg = proto.String(rel)
	}

	pid, _, err := s.cr.StartPageServerChld(opts)
	if err != nil {
		return err
	}

	s.process, err = os.FindProcess(pid)
	if err != nil {
		return err
	}

	return nil
}

func (s *PhaulServer) StopIter() error {
	state, err := s.process.Wait()
	if err != nil {
		return nil
	}

	if !state.Success() {
		return fmt.Errorf("page-server failed: %s", s)
	}
	return nil
}

/*
 * Server-local methods
 */
func (s *PhaulServer) LastImagesDir() string {
	return s.imgs.lastImagesDir()
}

func (s *PhaulServer) GetDir() string {
	return s.imgs.getParentPath()
}

func (s *PhaulServer) GetCriu() *criu.Criu {
	return s.cr
}

func (s *PhaulServer) IsLazy() bool {
	return s.cfg.Lazy
}
