package main

import (
  "log"
  "io/ioutil"
  "github.com/codegangsta/cli"
  "gopkg.in/yaml.v2"
  "fmt"
  "github.com/jmcvetta/napping"
)

type ServiceManifest struct {
  Image string `json:"image"`
  Expose int `json:"expose,omitempty"`
  Environment map[string]string `json:"environment,omitempty"`
  Require struct {
    Cpu int `json:"cpu,omitempty"`
    Mem int `json:"mem,omitempty"`
  } `json:"require,omitempty"`
}


func registerCommit(c *cli.Context, deployKey, commitSha, image string) {
  url := fmt.Sprintf("http://%s/deploy/register", c.GlobalString("api-endpoint"))

  var payload struct {
    DeployKey string `json:"deploy_key"`
    CommitSha string `json:"commit_sha"`
    Manifest  ServiceManifest `json:"manifest"`
  }

  payload.DeployKey = deployKey
  payload.CommitSha = commitSha

  data, err := ioutil.ReadFile(c.String("manifest-file"))
  if err != nil {
    log.Panic("mikro-cli: register-commit: cannot read manifest: ", err)
  }
  err = yaml.Unmarshal(data, &payload.Manifest)
  if err != nil {
    log.Panic("mikro-cli: register-commit: cannot decode manifest: ", err)
  }

  payload.Manifest.Image = image
  _, err = napping.Post(url, &payload, nil, nil)
  if err != nil {
    log.Panic("mikro-cli: register-commit: register failed: ", err)
  }
}

func cmdRegisterCommit(c *cli.Context) {
  registerCommit(c, c.String("deploy-key"), c.String("commit-sha"),
                 c.Args().First())
}


func main() {
  app := cli.NewApp()
  app.Name = "mikro-cli"
  app.Usage = "command-line interface to mikro.io"
  app.Action = func(c *cli.Context) {
    println("boom! I say!")
  }
  app.Flags = []cli.Flag{
      cli.StringFlag{
        Name: "api-endpoint",
        Value: "api.mikro.io",
      },
  }
  app.Commands = []cli.Command{
    {
      Name: "register-commit",
      Action: cmdRegisterCommit,
      Usage: "register an image and manifest with a commit",
      Flags: []cli.Flag {
        cli.StringFlag{
          Name: "deploy-key, k",
          EnvVar: "MIKRO_DEPLOY_KEY",
        },
        cli.StringFlag{
          Name: "commit-sha, s",
        },
        cli.StringFlag{
          Name: "manifest-file",
          Value: "mikro.yml",
        },
      },
    },
    {
      Name: "push",
      Action: cmdPush,
      Usage: "push image and register commit",
      Flags: []cli.Flag {
        cli.StringFlag{
          Name: "deploy-key, k",
          EnvVar: "MIKRO_DEPLOY_KEY",
        },
        cli.StringFlag{
          Name: "commit-sha, s",
        },
        cli.StringFlag{
          Name: "manifest-file",
          Value: "mikro.yml",
        },
      },
    },
  }

  app.RunAndExitOnError()
}
