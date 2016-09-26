package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var cmdPath = "./cmd"

func main() {
	svc := ec2.New(session.New(), &aws.Config{Region: aws.String("us-west-2")})

	// Find a running instance to use
	inst, err := findRunningInstance(svc)
	if err != nil {
		// No running instance exists, create one
		fmt.Println("No instance found, creating...")
		inst, err = createInstance(svc)
		check(err)
		fmt.Println("Created new instance")

		params := &ec2.DescribeInstancesInput{
			Filters: []*ec2.Filter{
				&ec2.Filter{
					Name:   aws.String("instance-id"),
					Values: []*string{inst.InstanceId},
				},
			},
		}

		// Wait until the instance is up and running
		fmt.Println("Waiting for instance to be ready...")
		svc.WaitUntilInstanceRunning(params)
		fmt.Println("Instance ready")

		res, err := svc.DescribeInstances(params)
		check(err)
		inst = res.Reservations[0].Instances[0]

	} else {
		fmt.Printf("Running instance found\n")
	}

	fmt.Printf("Using instance '%s'\n", *inst.PublicIpAddress)

	// Execute commands
	file, err := os.Open(cmdPath)
	check(err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		cmd := scanner.Text()
		res, err := execCmd(inst, cmd)
		check(err)
		fmt.Printf("\n> %s\n%s", cmd, *res)
	}
	err = scanner.Err()
	check(err)
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func createInstance(svc *ec2.EC2) (*ec2.Instance, error) {
	params := &ec2.RunInstancesInput{
		ImageId:          aws.String("ami-d732f0b7"), // Ubuntu
		InstanceType:     aws.String("t2.micro"),
		MaxCount:         aws.Int64(1),
		MinCount:         aws.Int64(1),
		KeyName:          aws.String("cc_assignment0"),
		SecurityGroupIds: []*string{aws.String("sg-e26fb89b")},
	}
	res, err := svc.RunInstances(params)
	return res.Instances[0], err
}

func findRunningInstance(svc *ec2.EC2) (*ec2.Instance, error) {
	resp, err := svc.DescribeInstances(nil)
	if err != nil {
		return nil, err
	}

	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			if *inst.State.Name == "running" {
				return inst, err
			}
		}
	}

	return nil, fmt.Errorf("Could not find running instance.")
}

func execCmd(inst *ec2.Instance, cmd string) (*string, error) {
	// Open PEM file
	pemPath := os.Getenv("PEM_PATH")
	pemBytes, err := ioutil.ReadFile(pemPath)
	if err != nil {
		return nil, err
	}

	// Obtain private key
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}

	// Connect to the remote server and perform the SSH handshake
	config := &ssh.ClientConfig{
		User:    "ubuntu",
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: 5 * time.Second,
	}
	fmt.Printf("Executing command: %s\n", cmd)
	addr := fmt.Sprintf("%s:%d", *inst.PublicIpAddress, 22)

	// Retry SSH until successful
	var conn *ssh.Client
	try, max, interval := 1, 5, 10*time.Second
	for conn == nil && try <= max {
		conn, err = ssh.Dial("tcp", addr, config)
		if err != nil {
			// Timeout occurred
			fmt.Printf("%v (%d/%d), trying again in %v...\n", err, try, max, interval)
			time.Sleep(interval)
		}
		try++
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return nil, err
	}

	defer session.Close()
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	err = session.Run(cmd)
	if err != nil {
		return nil, err
	}

	return aws.String(stdoutBuf.String()), nil
}
