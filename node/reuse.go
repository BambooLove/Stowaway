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

var VALIDMESSAGE string
var READYMESSAGE string

//reuse模式下的共用代码

/*-------------------------端口复用模式下节点主动连接功能代码--------------------------*/

// SetValidtMessage 设置启动认证密钥
func SetValidtMessage(key []byte) {
	firstSecret := utils.GetStringMd5(string(key))
	secondSecret := utils.GetStringMd5(firstSecret)
	finalSecret := firstSecret[:24] + secondSecret[:24]
	VALIDMESSAGE = finalSecret[8:16]
	READYMESSAGE = finalSecret[0:8]
}

// StartNodeConnReuse 初始化时的连接
func StartNodeConnReuse(monitor string, listenPort string, nodeid string, proxy, proxyU, proxyP string, key []byte) (net.Conn, string, error) {
	for {
		var controlConnToUpperNode net.Conn
		var err error

		if proxy == ""{
			controlConnToUpperNode, err = net.Dial("tcp", monitor)
		} else {
			controlConnToUpperNode, err = DialViaProxy(monitor,proxy,proxyU, proxyP)
		}

		if err != nil {
			log.Printf("[*]Connection refused! err:%s\n",err)
			return controlConnToUpperNode, "", err
		}

		err = IfValid(controlConnToUpperNode)
		if err != nil {
			controlConnToUpperNode.Close()
			continue
		}

		utils.ConstructPayloadAndSend(controlConnToUpperNode, nodeid, "", "COMMAND", "STOWAWAYAGENT", " ", " ", 0, utils.AdminId, key, false)

		utils.ExtractPayload(controlConnToUpperNode, key, utils.AdminId, true)

		err = utils.ConstructPayloadAndSend(controlConnToUpperNode, nodeid, "", "COMMAND", "INIT", " ", listenPort, 0, utils.AdminId, key, false)
		if err != nil {
			log.Printf("[*]Error occured: %s", err)
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
}

// ConnectNextNodeReuse connect命令时的连接
func ConnectNextNodeReuse(target string, nodeid string, key []byte) bool {
	for {
		controlConnToNextNode, err := net.Dial("tcp", target)

		if err != nil {
			return false
		}

		err = IfValid(controlConnToNextNode)
		if err != nil {
			controlConnToNextNode.Close()
			continue
		}

		utils.ConstructPayloadAndSend(controlConnToNextNode, nodeid, "", "COMMAND", "STOWAWAYAGENT", " ", " ", 0, utils.AdminId, key, false)

		for {
			command, err := utils.ExtractPayload(controlConnToNextNode, key, utils.AdminId, true)
			if err != nil {
				log.Println("[*]", err)
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
}

/*-------------------------端口复用模式下判断流量、转发流量功能代码--------------------------*/

// IfValid 发送特征字段
func IfValid(conn net.Conn) error {
	var NOT_VALID = errors.New("Not valid")

	//发送标志字段
	conn.Write([]byte(VALIDMESSAGE))

	returnMess := make([]byte, 8)
	_, err := io.ReadFull(conn, returnMess)

	if err != nil {
		conn.Close()
		return NOT_VALID
	}

	//检查返回字段
	if string(returnMess) != READYMESSAGE {
		return NOT_VALID
	} else {
		return nil
	}
}

// CheckValid 检查特征字符串
func CheckValid(conn net.Conn, reuse bool, report string) error {
	var NOT_VALID = errors.New("Not valid")

	defer conn.SetReadDeadline(time.Time{})
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	message := make([]byte, 8)
	count, err := io.ReadFull(conn, message)
	//防止如果复用的是mysql的情况，因为mysql是服务端先发送握手初始化消息
	if err != nil {
		if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
			if reuse {
				go ProxyStream(conn, message[:count], report)
			}
			return NOT_VALID
		} else {
			conn.Close()
			return NOT_VALID
		}
	}

	if string(message) == VALIDMESSAGE {
		conn.Write([]byte(READYMESSAGE))
		return nil
	} else {
		if reuse {
			go ProxyStream(conn, message, report)
		}
		return NOT_VALID
	}
}

// ProxyStream 不是来自Stowaway的连接，进行代理
func ProxyStream(conn net.Conn, message []byte, report string) {
	reuseAddr := fmt.Sprintf("127.0.0.1:%s", report)

	reuseConn, err := net.Dial("tcp", reuseAddr)

	if err != nil {
		fmt.Println(err)
		return
	}
	//把读出来的字节“归还”回去
	reuseConn.Write(message)

	go CopyTraffic(conn, reuseConn)
	CopyTraffic(reuseConn, conn)
}

// CopyTraffic 将流量代理至正确的port
func CopyTraffic(input, output net.Conn) {
	defer input.Close()

	buf := make([]byte, 10240)

	for {
		count, err := input.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				output.Write(buf[:count])
			}
			break
		}
		if count > 0 {
			output.Write(buf[:count])
		}
	}

	return
}
