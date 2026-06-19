package ldap

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
)

type Config struct {
	ServerAddress     string             `json:"serverAddress"`
	ServerPort        int                `json:"serverPort"`
	UseSSL            bool               `json:"connection_useSsl"`
	UseTLS            bool               `json:"connection_useTls"`
	StartTLS          bool               `json:"connection_startTls"`
	AllowUnauthorized bool               `json:"connection_allowUnauthorizedCerts"`
	BindingType       string             `json:"bindingType"`
	AdminDN           string             `json:"adminDn"`
	AdminPassword     string             `json:"adminPassword"`
	BaseDN            string             `json:"searchBase"`
	SearchFilter      string             `json:"searchFilter"`
	UserFilter        string             `json:"userFilter"`
	LoginIDAttr       string             `json:"loginIdAttribute"`
	FirstNameAttr     string             `json:"firstNameAttribute"`
	LastNameAttr      string             `json:"lastNameAttribute"`
	EmailAttr         string             `json:"emailAttribute"`
	GroupAttr         string             `json:"groupsAttribute"`
	SyncEnabled       bool               `json:"syncEnabled"`
	SyncInterval      string             `json:"syncInterval"`
	GroupFilter       string             `json:"groupFilter"`
	GroupRoleMapping  []GroupRoleMapping `json:"roleMapping"`
}

type GroupRoleMapping struct {
	LDAPGroup string `json:"ldapGroup"`
	N8NRole   string `json:"n8nRole"`
}

type User struct {
	DN        string   `json:"dn"`
	UID       string   `json:"uid"`
	Email     string   `json:"email"`
	FirstName string   `json:"firstName"`
	LastName  string   `json:"lastName"`
	Groups    []string `json:"groups"`
	Role      string   `json:"role"`
}

type Client struct {
	config Config
}

func NewClient(cfg Config) *Client {
	cfg = defaults(cfg)
	return &Client{config: cfg}
}

func (c *Client) AuthenticateUser(ctx context.Context, username string, password string) (*User, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := c.bind(conn); err != nil {
		return nil, fmt.Errorf("bind admin: %w", err)
	}
	filter := c.userSearchFilter(username)
	search := goldap.NewSearchRequest(c.config.BaseDN, goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 1, 0, false, filter, c.attributes(), nil)
	result, err := conn.SearchWithPaging(search, 1)
	if err != nil {
		return nil, err
	}
	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("ldap user not found")
	}
	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("ldap credentials rejected")
	}
	user := c.userFromEntry(entry)
	user.UID = firstNonEmpty(user.UID, username)
	user.Role = c.MapRole(user.Groups)
	return user, nil
}

func (c *Client) GetUsers(ctx context.Context) ([]*User, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := c.bind(conn); err != nil {
		return nil, err
	}
	filter := c.config.UserFilter
	if filter == "" {
		filter = fmt.Sprintf("(%s=*)", c.config.LoginIDAttr)
	}
	search := goldap.NewSearchRequest(c.config.BaseDN, goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 0, 0, false, filter, c.attributes(), nil)
	result, err := conn.Search(search)
	if err != nil {
		return nil, err
	}
	users := make([]*User, 0, len(result.Entries))
	for _, entry := range result.Entries {
		user := c.userFromEntry(entry)
		user.Role = c.MapRole(user.Groups)
		users = append(users, user)
	}
	return users, nil
}

func (c *Client) TestConnection(ctx context.Context) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	return c.bind(conn)
}

func (c *Client) MapRole(groups []string) string {
	for _, mapping := range c.config.GroupRoleMapping {
		for _, group := range groups {
			if sameGroup(group, mapping.LDAPGroup) {
				return firstNonEmpty(mapping.N8NRole, "global:member")
			}
		}
	}
	return "global:member"
}

func (c *Client) UserSearchFilter(username string) string {
	return c.userSearchFilter(username)
}

func (c *Client) connect() (*goldap.Conn, error) {
	port := c.config.ServerPort
	if port == 0 {
		if c.config.UseSSL {
			port = 636
		} else {
			port = 389
		}
	}
	address := fmt.Sprintf("%s:%d", c.config.ServerAddress, port)
	tlsConfig := &tls.Config{ServerName: c.config.ServerAddress, InsecureSkipVerify: c.config.AllowUnauthorized}
	if c.config.UseSSL || c.config.UseTLS {
		return goldap.DialTLS("tcp", address, tlsConfig)
	}
	conn, err := goldap.DialURL("ldap://" + address)
	if err != nil {
		return nil, err
	}
	if c.config.StartTLS {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

func (c *Client) bind(conn *goldap.Conn) error {
	if strings.EqualFold(c.config.BindingType, "anonymous") {
		return conn.UnauthenticatedBind("")
	}
	return conn.Bind(c.config.AdminDN, c.config.AdminPassword)
}

func (c *Client) userSearchFilter(username string) string {
	if c.config.SearchFilter != "" {
		return strings.ReplaceAll(c.config.SearchFilter, "{username}", goldap.EscapeFilter(username))
	}
	return fmt.Sprintf("(%s=%s)", c.config.LoginIDAttr, goldap.EscapeFilter(username))
}

func (c *Client) userFromEntry(entry *goldap.Entry) *User {
	return &User{
		DN:        entry.DN,
		UID:       entry.GetAttributeValue(c.config.LoginIDAttr),
		Email:     entry.GetAttributeValue(c.config.EmailAttr),
		FirstName: entry.GetAttributeValue(c.config.FirstNameAttr),
		LastName:  entry.GetAttributeValue(c.config.LastNameAttr),
		Groups:    entry.GetAttributeValues(c.config.GroupAttr),
	}
}

func (c *Client) attributes() []string {
	return []string{c.config.LoginIDAttr, c.config.EmailAttr, c.config.FirstNameAttr, c.config.LastNameAttr, c.config.GroupAttr}
}

func defaults(cfg Config) Config {
	if cfg.LoginIDAttr == "" {
		cfg.LoginIDAttr = "uid"
	}
	if cfg.EmailAttr == "" {
		cfg.EmailAttr = "mail"
	}
	if cfg.FirstNameAttr == "" {
		cfg.FirstNameAttr = "givenName"
	}
	if cfg.LastNameAttr == "" {
		cfg.LastNameAttr = "sn"
	}
	if cfg.GroupAttr == "" {
		cfg.GroupAttr = "memberOf"
	}
	return cfg
}

func sameGroup(value string, expected string) bool {
	if expected == "" {
		return false
	}
	if strings.EqualFold(value, expected) {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(expected))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
