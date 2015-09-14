package main

import (
  "log"
  "os"
  "io"
  "io/ioutil"
  "path/filepath"
  "github.com/codegangsta/cli"
  "encoding/json"
  "errors"
  "fmt"
  "github.com/docker/libtrust"
  "github.com/docker/docker/image"
  "github.com/docker/docker/pkg/parsers"
  "github.com/docker/distribution/digest"
  "github.com/cheggaaa/pb"
  "github.com/AdRoll/goamz/aws"
  "github.com/jmcvetta/napping"
  "github.com/docker/distribution"
  "github.com/docker/distribution/manifest"
  "github.com/docker/distribution/manifest/schema1"
  "github.com/docker/distribution/context"
  "github.com/docker/distribution/registry/storage"
  "github.com/docker/distribution/registry/storage/driver/s3"
  "github.com/fsouza/go-dockerclient"
)

type ImageBundle struct {
  imageDir string
  repositories map[string]map[string]string
  layers []string
}

func NewImageBundle(repo, tag string) (*ImageBundle, error) {
  var ib ImageBundle

  // Start to export the image!
  imageDir, err := ioutil.TempDir("", "mikro-cli-import-")
  if err != nil {
    log.Panic("mikro-cli: push: cannot create temp dir: ", err)
    return nil, err
  }
  ib.imageDir = imageDir

  err = ib.exportAndUntarImage(repo, tag)
  if err != nil {
    log.Panic("mikro-cli: push: cannot export image: ", err)
    ib.Close()
    return nil, err
  }

  reposJSON, err := ioutil.ReadFile(filepath.Join(ib.imageDir, "repositories"))
  if err != nil {
    log.Printf("no repositories data")

    ib.Close()
    return nil, err
  }

  err = json.Unmarshal(reposJSON, &ib.repositories)
  if err != nil {
    log.Printf("no unmarshall")

    ib.Close()
    return nil, err
  }

  return &ib, nil
}

func (ib *ImageBundle) GetImage(repo, tag string) (*image.Image, error) {
  tagMap, ok := ib.repositories[repo]
  if !ok {
    return nil, errors.New("no such repository")
  }
  layerId, ok := tagMap[tag]
  if !ok {
    return nil, errors.New("no such tag")
  }
  return ib.GetLayer(layerId)
}

func (ib *ImageBundle) GetLayer(address string) (*image.Image, error) {
  imageJSON, _ := ib.GetLayerJSON(address)
  return image.NewImgJSON(imageJSON)
}

func (ib *ImageBundle) GetLayerJSON(address string) ([]byte, error) {
  return ioutil.ReadFile(filepath.Join(ib.imageDir, address, "json"))
}

func (ib *ImageBundle) GetDigest(address string) (*digest.Digest, error){
  layer, err := os.Open(filepath.Join(ib.imageDir, address, "layer.tar"))
  if err != nil {
    return nil, err
  }
  defer layer.Close()
  digester := digest.Canonical.New()
  _, err = io.Copy(digester.Hash(), layer)
  dgst := digester.Digest()
  return &dgst, nil
}

func (ib *ImageBundle) Close() {
  os.RemoveAll(ib.imageDir)
}

func (ib *ImageBundle) exportAndUntarImage(repo, tag string) error {
  tmpArchive, err := ioutil.TempFile("", "mikro-cli-archive-")
  if err != nil {
    log.Printf("no create file")
    return err
  }
  defer tmpArchive.Close()
  defer os.Remove(tmpArchive.Name())

  err = ib.exportImage(repo, tag, tmpArchive)
  if err != nil {
    log.Printf("no export")
    return err
  }

  fp, err := os.Open(tmpArchive.Name())
  if err != nil {
    log.Printf("no open")
    return err
  }
  defer fp.Close()

  err = Untar(fp, ib.imageDir)
  if err != nil {
    log.Printf("no untar")
    return err
  }

  return nil
}

func (ib *ImageBundle) exportImage(repo, tag string, outputStream io.Writer) error {
  client, err := docker.NewClientFromEnv()
  if err != nil {
    return err
  }

  options := docker.ExportImageOptions{
    Name: repo + ":" + tag,
    OutputStream: outputStream,
  }

  return client.ExportImage(options)
}

func cmdPush(c *cli.Context) {
  url := fmt.Sprintf("http://%s/deploy/upload", c.GlobalString("api-endpoint"))

  var payload struct {
    DeployKey string `json:"deploy_key"`
  }

  payload.DeployKey = c.String("deploy-key")

  var response struct {
    AccessKey string `json:"access_key_id"`
    SecretKey string `json:"secret_access_key"`
    Bucket string `json:"bucket"`
    Region string `json:"region"`
    Name string `json:"name"`
  }

  _, err := napping.Post(url, &payload, &response, nil)
  if err != nil {
    log.Panic("mikro-cli: push: upload-request: ", err)
  }

  options := s3.DriverParameters{
    AccessKey: response.AccessKey,
    SecretKey: response.SecretKey,
    Bucket: response.Bucket,
    Region: aws.GetRegion(fmt.Sprint(response.Region)),
    ChunkSize: 5 << 21,
    Secure: true,
  }

  ctx := context.Background()

  driver, err := s3.New(options)
  registry, err := storage.NewRegistry(ctx, driver)

  imageName := c.Args()[0]
  repo, tag := parsers.ParseRepositoryTag(imageName)

  fmt.Printf("repo: %s tag: %s\n", repo, tag)

  bundle, err := NewImageBundle(repo, tag)
  if err != nil {
    log.Panic("mikro-cli: push: cannot export image: ", err)
  }
  defer bundle.Close()

  privateKey, err := libtrust.GenerateECP256PrivateKey()

  dgst, err := pushImageToRegistry(ctx, response.Name,
                                   c.String("commit-sha"),
                                   repo, tag,
                                   bundle,
                                   privateKey,
                                   registry)
  fmt.Printf("Pushed image %s as %s@%s\n", imageName, response.Name, dgst)

  // FIXME: we want to use the dgst here, but we can't since ECS doesn't
  // support it yet.
  registerCommit(c, c.String("deploy-key"), c.String("commit-sha"),
                 fmt.Sprintf("private:%s:%s", response.Name,
                             c.String("commit-sha")))
}

func pushImageToRegistry(ctx context.Context,
                         serviceName, commitSha string,
                         repo, tag string,
                         ib *ImageBundle,
                         privateKey libtrust.PrivateKey,
                         registry distribution.Namespace) (string, error) {
  repository, err := registry.Repository(ctx, serviceName)
  blobs := repository.Blobs(ctx)

  layer, err := ib.GetImage(repo, tag)
  if err != nil {
    return "", err
  }

  m := &schema1.Manifest{
    Versioned: manifest.Versioned{
      SchemaVersion: 1,
    },
    Name:         serviceName,
    Tag:          commitSha,
    Architecture: layer.Architecture,
    FSLayers:     []schema1.FSLayer{},
    History:      []schema1.History{},
  }

  for {
    jsonData, err := ib.GetLayerJSON(layer.ID)
    if err != nil {
      return "", err
    }

    dgst, err := ib.GetDigest(layer.ID)
    if err != nil {
      return "", err
    }

    fmt.Printf("Layer %s digest %s\n", layer.ID, dgst)

    var exists bool
    _, err = blobs.Stat(ctx, *dgst)
    switch err {
    case nil:
      exists = true
    case distribution.ErrBlobUnknown:
      // nop
    default:
    }

    if !exists {
      descriptor, err := pushLayer(ctx, layer.ID, ib.imageDir, blobs)
      if err != nil {
        return "", err
      }
      dgst = &descriptor.Digest
    }

    m.FSLayers = append(m.FSLayers, schema1.FSLayer{BlobSum: *dgst})
    m.History = append(m.History, schema1.History{V1Compatibility: string(jsonData)})

    if layer.Parent == "" {
      break
    }
    layer, err = ib.GetLayer(layer.Parent)
    if err != nil {
      return "", err
    }
  }

  // log.Printf("WE PUSHED: %v\n", m)
  return pushManifest(ctx, m, privateKey, repository)
}

func pushManifest(ctx context.Context, m *schema1.Manifest,
                  privateKey libtrust.PrivateKey,
                  repository distribution.Repository) (string, error) {
  signed, err := schema1.Sign(m, privateKey)
  if err != nil {
    return "", err
  }

  manifestDigest, err := digestFromManifest(signed)
  if err != nil {
    return "", err
  }

  manifests, err := repository.Manifests(ctx)
  if err != nil {
    return "", err
  }

  log.Printf("manifest: digest: %s", manifestDigest)

  return string(manifestDigest), manifests.Put(signed)
}

func pushLayer(ctx context.Context, address, imageDir string,
               blobs distribution.BlobStore) (descriptor distribution.Descriptor, err error) {
  fmt.Printf("push layer: %s", address)

  layer, err := os.Open(filepath.Join(imageDir, address, "layer.tar"))
  if err != nil {
    log.Printf("Error reading embedded tar: %v", err)
    return
  }
  defer layer.Close()

  stat, err := layer.Stat()

  digester := digest.Canonical.New()

  bar := pb.New(int(stat.Size())).SetUnits(pb.U_BYTES)
  bar.Start()

  writer, err := blobs.Create(ctx)
  if err != nil {
    log.Printf("Error creating blob writer: %v", err)
    return
  }

  _, err = io.Copy(io.MultiWriter(writer, bar, digester.Hash()), layer)
  if err != nil {
    log.Printf("Error copying to blob writer: %v", err)
    return
  }

  descriptor, err = writer.Commit(ctx, distribution.Descriptor{
    Digest: digester.Digest(),
  })
  if err != nil {
    log.Printf("Error commiting blob writer: %v", err)
    return
  }

  bar.Finish()
  return
}

func digestFromManifest(m *schema1.SignedManifest) (digest.Digest, error) {
  payload, err := m.Payload()
  if err != nil {
    return "", err
  }
  manifestDigest, err := digest.FromBytes(payload)
  return manifestDigest, nil
}
