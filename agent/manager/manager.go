/*
 * @Author: ph4ntom
 * @Date: 2021-03-23 19:01:26
 * @LastEditors: ph4ntom
 * @LastEditTime: 2021-04-01 14:19:09
 */
package manager

import (
	"Stowaway/protocol"
	"Stowaway/share"
	"net"
)

const (
	SOCKS = iota
	S_UPDATETCP
	S_UPDATEUDP
	S_UPDATEUDPHEADER
	S_GETTCPDATACHAN
	S_GETUDPCHANS
	S_GETUDPHEADER
	S_CLOSETCP
)

type Manager struct {
	//Fiel
	File *share.MyFile
	//Socks5
	socks             map[uint64]*socksStatus
	SocksTCPDataChan  chan interface{}
	SocksUDPDataChan  chan *protocol.SocksUDPData
	SocksUDPReadyChan chan *protocol.UDPAssRes
	//share
	TaskChan   chan *ManagerTask
	ResultChan chan *ManagerResult
	Done       chan bool
}

type ManagerTask struct {
	Category int
	Mode     int
	Seq      uint64
	//socks
	SocksSocket     net.Conn
	SocksListener   *net.UDPConn
	SocksHeaderAddr string
	SocksHeader     []byte
}

type ManagerResult struct {
	OK bool
	//socks
	SocksSeqExist  bool
	DataChan       chan []byte
	ReadyChan      chan string
	SocksID        uint64
	SocksUDPHeader []byte
}

type socksStatus struct {
	IsUDP bool
	tcp   *tcpSocks
	udp   *udpSocks
}

type tcpSocks struct {
	DataChan chan []byte
	Conn     net.Conn
}

type udpSocks struct {
	DataChan    chan []byte
	ReadyChan   chan string
	Listener    *net.UDPConn
	HeaderPairs map[string][]byte
}

func NewManager(file *share.MyFile) *Manager {
	manager := new(Manager)
	manager.File = file

	manager.socks = make(map[uint64]*socksStatus)
	manager.SocksTCPDataChan = make(chan interface{}, 5)
	manager.SocksUDPReadyChan = make(chan *protocol.UDPAssRes, 1)
	manager.SocksUDPDataChan = make(chan *protocol.SocksUDPData, 5)

	manager.ResultChan = make(chan *ManagerResult)
	manager.TaskChan = make(chan *ManagerTask)
	manager.Done = make(chan bool)
	return manager
}

func (manager *Manager) Run() {
	for {
		task := <-manager.TaskChan
		switch task.Category {
		case SOCKS:
			switch task.Mode {
			case S_GETTCPDATACHAN:
				manager.getTCPDataChan(task)
				<-manager.Done
			case S_GETUDPCHANS:
				manager.getUDPChans(task)
				<-manager.Done
			case S_GETUDPHEADER:
				manager.getUDPHeader(task)
			case S_UPDATETCP:
				manager.updateTCP(task)
			case S_UPDATEUDP:
				manager.updateUDP(task)
			case S_UPDATEUDPHEADER:
				manager.updateUDPHeader(task)
			case S_CLOSETCP:
				manager.closeTCP(task)
			}
		}
	}
}

func (manager *Manager) getTCPDataChan(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		manager.ResultChan <- &ManagerResult{
			SocksSeqExist: true,
			DataChan:      manager.socks[task.Seq].tcp.DataChan,
		}
	} else {
		manager.socks[task.Seq] = new(socksStatus)
		manager.socks[task.Seq].tcp = new(tcpSocks)
		manager.socks[task.Seq].tcp.DataChan = make(chan []byte, 5) // register it!
		manager.ResultChan <- &ManagerResult{
			SocksSeqExist: false,
			DataChan:      manager.socks[task.Seq].tcp.DataChan,
		} // tell upstream result
	}
}

func (manager *Manager) getUDPChans(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		manager.ResultChan <- &ManagerResult{
			OK:        true,
			DataChan:  manager.socks[task.Seq].udp.DataChan,
			ReadyChan: manager.socks[task.Seq].udp.ReadyChan,
		}
	} else {
		manager.ResultChan <- &ManagerResult{OK: false}
	}
}

func (manager *Manager) updateTCP(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		manager.socks[task.Seq].IsUDP = false
		manager.socks[task.Seq].tcp.Conn = task.SocksSocket
		manager.ResultChan <- &ManagerResult{OK: true}
	} else {
		manager.ResultChan <- &ManagerResult{OK: false} // avoid the scenario that admin conn ask to fin before "socks.buildConn()" call "updateTCP()"
	}
}

func (manager *Manager) updateUDP(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		manager.socks[task.Seq].IsUDP = true
		manager.socks[task.Seq].udp = new(udpSocks)
		manager.socks[task.Seq].udp.DataChan = make(chan []byte)
		manager.socks[task.Seq].udp.ReadyChan = make(chan string)
		manager.socks[task.Seq].udp.HeaderPairs = make(map[string][]byte)
		manager.socks[task.Seq].udp.Listener = task.SocksListener
		manager.ResultChan <- &ManagerResult{OK: true} // tell upstream work done
	} else {
		manager.ResultChan <- &ManagerResult{OK: false}
	}
}

func (manager *Manager) updateUDPHeader(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		manager.socks[task.Seq].udp.HeaderPairs[task.SocksHeaderAddr] = task.SocksHeader
	}
	manager.ResultChan <- &ManagerResult{}
}

func (manager *Manager) getUDPHeader(task *ManagerTask) {
	if _, ok := manager.socks[task.Seq]; ok {
		if _, ok := manager.socks[task.Seq].udp.HeaderPairs[task.SocksHeaderAddr]; ok {
			manager.ResultChan <- &ManagerResult{
				OK:             true,
				SocksUDPHeader: manager.socks[task.Seq].udp.HeaderPairs[task.SocksHeaderAddr],
			}
		} else {
			manager.ResultChan <- &ManagerResult{OK: false}
		}
	} else {
		manager.ResultChan <- &ManagerResult{OK: false}
	}
}

func (manager *Manager) closeTCP(task *ManagerTask) {
	if manager.socks[task.Seq].tcp.Conn != nil {
		manager.socks[task.Seq].tcp.Conn.Close() // avoid the scenario that admin conn ask to fin before "socks.buildConn()" call "updateTCP()"
	}

	close(manager.socks[task.Seq].tcp.DataChan)

	if manager.socks[task.Seq].IsUDP {
		manager.socks[task.Seq].udp.Listener.Close()
		close(manager.socks[task.Seq].udp.DataChan)
		close(manager.socks[task.Seq].udp.ReadyChan)
		manager.socks[task.Seq].udp.HeaderPairs = nil
	}

	delete(manager.socks, task.Seq) // upstream not waiting
}
