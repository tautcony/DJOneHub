package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// EmailChannel 通过 SMTP 发送通知邮件。只出不进。
type EmailChannel struct {
	cfg EmailSettings
}

var _ Channel = (*EmailChannel)(nil)

func NewEmailChannel(cfg EmailSettings) (*EmailChannel, error) {
	if cfg.SMTPHost == "" || cfg.SMTPPort <= 0 || cfg.FromAddress == "" || len(cfg.ToAddresses) == 0 {
		return nil, fmt.Errorf("email: smtp_host / smtp_port / from_address / to_addresses 均不能为空")
	}
	return &EmailChannel{cfg: cfg}, nil
}

func (c *EmailChannel) Name() string { return "email" }

func (c *EmailChannel) Send(ctx context.Context, msg Message) error {
	return c.send(ctx, msg)
}

func (c *EmailChannel) send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(c.cfg.SMTPHost, fmt.Sprintf("%d", c.cfg.SMTPPort))
	payload := c.buildMessage(msg)

	var auth smtp.Auth
	if c.cfg.Username != "" {
		auth = smtp.PlainAuth("", c.cfg.Username, c.cfg.Password, c.cfg.SMTPHost)
	}

	if !c.cfg.UseSSL {
		conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("SMTP 连接失败: %w", err)
		}
		return c.sendSMTP(ctx, conn, auth, payload)
	}

	// 隐式 TLS（通常是 465 端口）：先建立 TLS 连接再握手。
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP over TLS 连接失败: %w", err)
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: c.cfg.SMTPHost})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP over TLS 握手失败: %w", err)
	}
	return c.sendSMTP(ctx, tlsConn, auth, payload)
}

func (c *EmailChannel) sendSMTP(ctx context.Context, conn net.Conn, auth smtp.Auth, payload []byte) error {
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, c.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Close()
	if _, isTLS := conn.(*tls.Conn); !isTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: c.cfg.SMTPHost}); err != nil {
				return fmt.Errorf("SMTP STARTTLS 失败: %w", err)
			}
		}
	}

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("SMTP 服务器不支持认证")
		}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := client.Mail(c.cfg.FromAddress); err != nil {
		return err
	}
	for _, recipient := range c.cfg.ToAddresses {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func validateEmailAddress(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("email: %s 不得包含换行符", field)
	}
	if _, err := mail.ParseAddress(value); err != nil {
		return fmt.Errorf("email: %s 不是有效邮箱地址", field)
	}
	return nil
}

// buildMessage 构造 RFC 5322 邮件。主题用 RFC 2047 编码以支持中文。
func (c *EmailChannel) buildMessage(msg Message) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + c.cfg.FromAddress + "\r\n")
	sb.WriteString("To: " + strings.Join(c.cfg.ToAddresses, ", ") + "\r\n")
	sb.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", "[DJOneHub] "+msg.Subject()) + "\r\n")
	sb.WriteString("Date: " + msg.Timestamp.Format(time.RFC1123Z) + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	// 邮件正文要求 CRLF 换行。
	sb.WriteString(strings.ReplaceAll(msg.Text(), "\n", "\r\n"))
	sb.WriteString("\r\n")
	return []byte(sb.String())
}

func (c *EmailChannel) Close() error { return nil }
