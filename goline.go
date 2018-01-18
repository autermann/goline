package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"github.com/autermann/grovepi"
	"github.com/eclipse/paho.mqtt.golang"
)

// Client is the GoLine client.
type Client struct {
	Broker   string
	Topic    string
	HomePIN  byte
	GuestPIN byte
	client   mqtt.Client
	gp       *grovepi.GrovePi
	goals    chan string
	done     chan bool
}

// NewClient creates a new client.
func NewClient() (*Client, error) {
	var err error
	c := &Client{
		goals: make(chan string, 5),
	}

	c.gp, err = grovepi.NewGrovePi(0x04)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) readPin(pin byte) (uint32, error) {
	raw, err := c.gp.DigitalRead(pin)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(raw[1:5]), nil
}

func (c *Client) monitor(pin byte, who string, done chan bool) {
	last, err := c.readPin(pin)
	if err != nil {
		log.Printf("Can't read pin: %v", err)
	}
	for {
		select {
		case <-done:
			return
		case <-time.After(1 * time.Nanosecond):
			value, err := c.readPin(pin)
			if err != nil {
				log.Printf("Can't read pin: %v", err)
			} else if last != value {
				c.goals <- who
				last = value
			}
		}
	}
}

func (c *Client) listen(done chan bool) {
	for {
		select {
		case <-done:
			return
		case who := <-c.goals:
			err := c.publish(who)
			if err != nil {
				log.Printf("Can't send MQTT message: %v", err)
			}
		}
	}
}

func (c *Client) publish(who string) error {
	payload, err := json.Marshal(map[string]string{"scorer": who})
	if err != nil {
		return err
	}
	if t := c.client.Publish(c.Topic, 0, false, payload); t.Wait() && t.Error() != nil {
		return t.Error()
	}
	return nil
}

// Connect connects to the MQTT broker.
func (c *Client) Connect() error {
	if c.client != nil && c.client.IsConnected() {
		return errors.New("already connected")
	}
	mqtt.DEBUG = log.New(os.Stdout, "", 0)
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().
		AddBroker(c.Broker).
		SetClientID("goline").
		SetKeepAlive(2 * time.Second).
		SetPingTimeout(1 * time.Second)

	c.client = mqtt.NewClient(opts)

	if t := c.client.Connect(); t.Wait() && t.Error() != nil {
		return t.Error()
	}

	return nil
}

// Run starts this client and monitors the pins.
func (c *Client) Run() {
	c1 := make(chan bool)
	c2 := make(chan bool)
	c3 := make(chan bool)

	go c.listen(c1)
	go c.monitor(grovepi.D7, "home", c2)
	go c.monitor(grovepi.D8, "guest", c3)

	<-c.done

	c1 <- true
	c2 <- true
	c3 <- true

	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
	}
}

// Disconnect disconnects from the MQTT broker.
func (c *Client) Disconnect() {
	c.done <- true
}

func main() {
	client, err := NewClient()
	if err != nil {
		log.Fatal(err)
	}
	client.Broker = "tcp://localhost:1883"
	client.Topic = "goline/goals"
	client.HomePIN = grovepi.D7
	client.GuestPIN = grovepi.D8
	if err := client.Connect(); err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect()

	client.Run()
}
