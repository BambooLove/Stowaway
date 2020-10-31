package node

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"Stowaway/utils"
)

var (
	NodeInfo  *utils.NodeInfo
	NodeStuff *utils.NodeStuff
)

func init() {
	NodeStuff = utils.NewNodeStuff()
	NodeInfo = utils.NewNodeInfo()
}

/*-------------------------一般模式下初始化节点代码--------------------------*/

// StartNodeConn 初始化一个节点连接操作
func StartNodeConn(monitor string, listenPort string, nodeid string, proxy,proxyU,proxyP string,key []byte) (net.Conn, string, error) {
	var controlConnToUpperNode net.Conn
	var err error

	if proxy == ""{
		controlConnToUpperNode, err = net.Dial("tcp", monitor)
	} else {
		controlConnToUpperNode, err = DialViaProxy(monitor,proxy,proxyU,proxyP)
	}

	if err != nil {
		return controlConnToUpperNode, "", err
	}

	err = SendSecret(controlConnToUpperNode, key)
	if err != nil {
		return controlConnToUpperNode, "", err
	}

	utils.ConstructPayloadAndSend(controlConnToUpperNode, nodeid, "", "COMMAND", "STOWAWAYAGENT", " ", " ", 0, utils.AdminId, key, false)

	utils.ExtractPayload(controlConnToUpperNode, key, utils.AdminId, true)

	err = utils.ConstructPayloadAndSend(controlConnToUpperNode, nodeid, "", "COMMAND", "INIT", " ", listenPort, 0, utils.AdminId, key, false)
	if err != nil {
		return controlConnToUpperNode, "", err
	}
	//等待admin为其分配一个id号
	for {
		command, _ := utils.ExtractPayload(controlConnToUpperNode, key, utils.AdminId, true)
		switch command.Command {
		case "ID":
			nodeid = command.NodeId
			return controlConnToUpperNode, nodeid, nil
		}
	}
}

// StartNodeListen 初始化节点监听操作
func StartNodeListen(listenPort string, nodeid string, key []byte) {
	var newNodeMessage []byte

	if listenPort == "" { //如果没有port，直接退出
		return
	}

	listenAddr := fmt.Sprintf("0.0.0.0:%s", listenPort)
	waitingForLowerNode, err := net.Listen("tcp", listenAddr)

	if err != nil {
		log.Fatalf("[*]Cannot listen on port %s", listenPort)
	}

	for {
		connToLowerNode, err := waitingForLowerNode.Accept()
		if err != nil {
			log.Println("[*]", err)
			return
		}

		err = CheckSecret(connToLowerNode, key)
		if err != nil {
			log.Println("[*]", err)
			continue
		}

		for i := 0; i < 2; i++ {
			command, _ := utils.ExtractPayload(connToLowerNode, key, utils.AdminId, true)
			switch command.Command {
			case "STOWAWAYADMIN":
				utils.ConstructPayloadAndSend(connToLowerNode, nodeid, "", "COMMAND", "INIT", " ", listenPort, 0, utils.AdminId, key, false)
			case "ID":
				NodeStuff.ControlConnForLowerNodeChan <- connToLowerNode
				NodeStuff.NewNodeMessageChan <- newNodeMessage
				NodeStuff.IsAdmin <- true
			case "REONLINESUC":
				NodeStuff.Adminconn <- connToLowerNode
			case "STOWAWAYAGENT":
				if !NodeStuff.Offline {
					utils.ConstructPayloadAndSend(connToLowerNode, nodeid, "", "COMMAND", "CONFIRM", " ", " ", 0, nodeid, key, false)
				} else {
					utils.ConstructPayloadAndSend(connToLowerNode, nodeid, "", "COMMAND", "REONLINE", " ", listenPort, 0, nodeid, key, false)
				}
			case "INIT":
				//告知admin新节点消息
				newNodeMessage, _ = utils.ConstructPayload(utils.AdminId, "", "COMMAND", "NEW", " ", connToLowerNode.RemoteAddr().String(), 0, nodeid, key, false)

				NodeInfo.LowerNode.Payload[utils.AdminId] = connToLowerNode //将这个socket用0号位暂存，等待admin分配完id后再将其放入对应的位置
				NodeStuff.ControlConnForLowerNodeChan <- connToLowerNode
				NodeStuff.NewNodeMessageChan <- newNodeMessage //被连接后不终止监听，继续等待可能的后续节点连接，以此组成树状结构

				NodeStuff.IsAdmin <- false
			}
		}
	}
}

/*-------------------------节点主动connect模式代码--------------------------*/

// ConnectNextNode connect命令代码
func ConnectNextNode(target string, nodeid string, key []byte) bool {
	controlConnToNextNode, err := net.Dial("tcp", target)

	if err != nil {
		return false
	}

	err = SendSecret(controlConnToNextNode, key)
	if err != nil {
		return false
	}

	utils.ConstructPayloadAndSend(controlConnToNextNode, nodeid, "", "COMMAND", "STOWAWAYAGENT", " ", " ", 0, utils.AdminId, key, false)

	for {
		command, err := utils.ExtractPayload(controlConnToNextNode, key, utils.AdminId, true)
		if err != nil {
			return false
		}

		switch command.Command {
		case "INIT":
			//类似与上面
			newNodeMessage, _ := utils.ConstructPayload(utils.AdminId, "", "COMMAND", "NEW", " ", controlConnToNextNode.RemoteAddr().String(), 0, nodeid, key, false)

			NodeInfo.LowerNode.Payload[utils.AdminId] = controlConnToNextNode
			NodeStuff.ControlConnForLowerNodeChan <- controlConnToNextNode
			NodeStuff.NewNodeMessageChan <- newNodeMessage
			NodeStuff.IsAdmin <- false

			return true
		case "REONLINE":
			//普通节点重连
			NodeStuff.ReOnlineID <- command.CurrentId
			NodeStuff.ReOnlineConn <- controlConnToNextNode

			<-NodeStuff.PrepareForReOnlineNodeReady

			utils.ConstructPayloadAndSend(controlConnToNextNode, nodeid, "", "COMMAND", "REONLINESUC", " ", " ", 0, nodeid, key, false)

			return true
		}
	}
}

/*-------------------------节点被动模式下功能代码--------------------------*/

// AcceptConnFromUpperNode 被动模式下startnode接收admin重连 && 普通节点被动启动等待上级节点主动连接
func AcceptConnFromUpperNode(listenPort string, nodeid string, key []byte) (net.Conn, string) {
	listenAddr := fmt.Sprintf("0.0.0.0:%s", listenPort)
	waitingForConn, err := net.Listen("tcp", listenAddr)

	if err != nil {
		log.Fatalf("[*]Cannot listen on port %s", listenPort)
	}

	for {
		comingConn, err := waitingForConn.Accept()
		if err != nil {
			log.Println("[*]", err)
			continue
		}

		err = CheckSecret(comingConn, key)
		if err != nil {
			log.Println("[*]", err)
			continue
		}

		utils.ExtractPayload(comingConn, key, utils.AdminId, true)

		utils.ConstructPayloadAndSend(comingConn, nodeid, "", "COMMAND", "INIT", " ", listenPort, 0, utils.AdminId, key, false)

		command, _ := utils.ExtractPayload(comingConn, key, utils.AdminId, true) //等待分配id
		if command.Command == "ID" {
			nodeid = command.NodeId
			waitingForConn.Close()
			return comingConn, nodeid
		}

	}

}

/*-------------------------一般模式及被动模式下(除了reuse模式)节点互相校验代码--------------------------*/

// SendSecret 发送secret值
func SendSecret(conn net.Conn, key []byte) error {
	var NOT_VALID = errors.New("Not valid secret,check the secret!")

	defer conn.SetReadDeadline(time.Time{})
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	secret := utils.GetStringMd5(string(key))
	conn.Write([]byte(secret[:16]))

	buffer := make([]byte, 16)
	count, err := io.ReadFull(conn, buffer)

	if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
		conn.Close()
		return NOT_VALID
	}

	if err != nil {
		conn.Close()
		return NOT_VALID
	}

	if string(buffer[:count]) == secret[:16] {
		return nil
	}
	//不合法的连接，直接关闭
	conn.Close()

	return NOT_VALID
}

// CheckSecret 检查secret值，在连接建立前测试合法性
func CheckSecret(conn net.Conn, key []byte) error {
	var NOT_VALID = errors.New("Not valid secret,check the secret!")

	defer conn.SetReadDeadline(time.Time{})
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	secret := utils.GetStringMd5(string(key))

	buffer := make([]byte, 16)
	count, err := io.ReadFull(conn, buffer)

	if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
		conn.Close()
		return NOT_VALID
	}

	if err != nil {
		conn.Close()
		return NOT_VALID
	}

	if string(buffer[:count]) == secret[:16] {
		conn.Write([]byte(secret[:16]))
		return nil
	}

	conn.Close()

	return NOT_VALID
}
