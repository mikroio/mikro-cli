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
  Image string
  Expose int
  Environment map[string]string
  Require struct {
    Cpu int
    Mem int
  }
}


func registerCommit(c *cli.Context) {
  url := fmt.Sprintf("http://%s/deploy/register", c.GlobalString("api-endpoint"))

  var payload struct {
    DeployKey string `json:"deploy_key"`
    CommitSha string `json:"commit_sha"`
    Manifest  ServiceManifest `json:"manifest"`
  }

  payload.DeployKey = c.String("deploy-key")
  payload.CommitSha = c.String("commit-sha")

  data, err := ioutil.ReadFile(c.String("manifest-file"))
  if err != nil {
    log.Panic("mikro-cli: register-commit: cannot read manifest: ", err)
  }
  err = yaml.Unmarshal(data, &payload.Manifest)
  if err != nil {
    log.Panic("mikro-cli: register-commit: cannot decode manifest: ", err)
  }

  image := payload.Manifest.Image
  if image == "" {
    image = c.Args().First()
    if image == "" {
      log.Panic("mikro-cli: register-commit: no image specified in manifest, or on command-line")
    }
    payload.Manifest.Image = image
  }

  _, err = napping.Post(url, &payload, nil, nil)
  if err != nil {
    log.Panic("mikro-cli: register-commit: register failed: ", err)
  }
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
      Action: registerCommit,
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
  }

  app.RunAndExitOnError()
}
