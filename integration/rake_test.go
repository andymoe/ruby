package integration_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/paketo-buildpacks/occam"
	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
	. "github.com/paketo-buildpacks/occam/matchers"
)

func testRake(t *testing.T, context spec.G, it spec.S) {
	var (
		Expect     = NewWithT(t).Expect
		Eventually = NewWithT(t).Eventually

		pack   occam.Pack
		docker occam.Docker
	)

	it.Before(func() {
		pack = occam.NewPack()
		docker = occam.NewDocker()
	})

	context("when building a rake container", func() {
		var (
			image     occam.Image
			container occam.Container

			name   string
			source string
		)

		it.Before(func() {
			var err error
			name, err = occam.RandomName()
			Expect(err).NotTo(HaveOccurred())
		})

		it.After(func() {
			Expect(docker.Container.Remove.Execute(container.ID)).To(Succeed())
			Expect(docker.Image.Remove.Execute(image.ID)).To(Succeed())
			Expect(docker.Volume.Remove.Execute(occam.CacheVolumeNames(name))).To(Succeed())
			Expect(os.RemoveAll(source)).To(Succeed())
		})

		context("uses the rake gem", func() {
			it("creates a working OCI image", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "rake"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.WithNoColor().Build.
					WithBuildpacks(rubyBuildpack).
					WithPullPolicy("never").
					Execute(name, source)
				Expect(err).NotTo(HaveOccurred(), logs.String())

				container, err = docker.Container.Run.Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				rLogs := func() fmt.Stringer {
					rakeLogs, err := docker.Container.Logs.Execute(container.ID)
					Expect(err).NotTo(HaveOccurred())
					return rakeLogs
				}

				Eventually(rLogs).Should(ContainSubstring("I am a rake task"))

				Expect(logs).To(ContainLines(ContainSubstring("MRI Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Bundler Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Bundle Install Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Rake Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("bundle exec rake")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Procfile Buildpack")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Environment Variables Buildpack")))
			})
		})

		context("does not use rake gem", func() {
			it("creates a working OCI image", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "rake_no_gem"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.WithNoColor().Build.
					WithBuildpacks(rubyBuildpack).
					WithPullPolicy("never").
					Execute(name, source)
				Expect(err).NotTo(HaveOccurred(), logs.String())

				container, err = docker.Container.Run.Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				rLogs := func() fmt.Stringer {
					rakeLogs, err := docker.Container.Logs.Execute(container.ID)
					Expect(err).NotTo(HaveOccurred())
					return rakeLogs
				}

				Eventually(rLogs).Should(ContainSubstring("I am a rake task"))

				Expect(logs).To(ContainLines(ContainSubstring("MRI Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Rake Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("rake")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Bundler Buildpack")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Bundle Install Buildpack")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Procfile Buildpack")))
				Expect(logs).NotTo(ContainLines(ContainSubstring("Image Labels Buildpack")))
			})
		})

		context("using optional utility buildpacks", func() {
			it.Before(func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "rake"))
				Expect(err).NotTo(HaveOccurred())
				Expect(ioutil.WriteFile(filepath.Join(source, "Procfile"), []byte("web: bundle exec rake proc"), 0644)).To(Succeed())
			})

			it("builds a working image that complies with utility buildpack functions", func() {
				var err error
				var logs fmt.Stringer
				image, logs, err = pack.WithNoColor().Build.
					WithBuildpacks(rubyBuildpack).
					WithPullPolicy("never").
					WithEnv(map[string]string{
						"BPE_SOME_VARIABLE": "SOME_VALUE",
						"BP_IMAGE_LABELS":   "some-label=some-value",
					}).
					Execute(name, source)
				Expect(err).NotTo(HaveOccurred(), logs.String())

				container, err = docker.Container.Run.Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				Expect(image.Buildpacks[6].Key).To(Equal("paketo-buildpacks/environment-variables"))
				Expect(image.Buildpacks[6].Layers["environment-variables"].Metadata["variables"]).To(Equal(map[string]interface{}{"SOME_VARIABLE": "SOME_VALUE"}))

				rLogs := func() fmt.Stringer {
					rakeLogs, err := docker.Container.Logs.Execute(container.ID)
					Expect(err).NotTo(HaveOccurred())
					return rakeLogs
				}

				Eventually(rLogs).Should(ContainSubstring("I am the proc rake task"))

				Expect(logs).To(ContainLines(ContainSubstring("MRI Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Bundler Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Bundle Install Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Rake Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Procfile Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Image Labels Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Environment Variables Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("bundle exec rake proc")))

				Expect(image.Labels["some-label"]).To(Equal("some-value"))
			})
		})

		context("when using CA certificates", func() {
			var (
				client *http.Client
			)

			it.Before(func() {
				var err error
				name, err = occam.RandomName()
				Expect(err).NotTo(HaveOccurred())
				source, err = occam.Source(filepath.Join("testdata", "ca_cert_app"))
				Expect(err).NotTo(HaveOccurred())

				caCert, err := ioutil.ReadFile(fmt.Sprintf("%s/certs/ca.pem", source))
				Expect(err).ToNot(HaveOccurred())

				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(caCert)

				cert, err := tls.LoadX509KeyPair(fmt.Sprintf("%s/certs/cert.pem", source), fmt.Sprintf("%s/certs/key.pem", source))
				Expect(err).ToNot(HaveOccurred())

				client = &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							RootCAs:      caCertPool,
							Certificates: []tls.Certificate{cert},
							MinVersion:   tls.VersionTLS12,
						},
					},
				}
			})

			it("builds a working OCI image with given CA cert added to trust store", func() {
				var err error
				var logs fmt.Stringer
				image, logs, err = pack.WithNoColor().Build.
					WithBuildpacks(rubyBuildpack).
					WithPullPolicy("never").
					Execute(name, source)
				Expect(err).NotTo(HaveOccurred())

				Expect(logs).To(ContainLines(ContainSubstring("CA Certificates Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("MRI Buildpack")))
				Expect(logs).To(ContainLines(ContainSubstring("Rake Buildpack")))

				container, err = docker.Container.Run.
					WithPublish("8080").
					WithEnv(map[string]string{
						"PORT":                 "8080",
						"SERVICE_BINDING_ROOT": "/bindings",
					}).
					WithVolume(fmt.Sprintf("%s/binding:/bindings/ca-certificates", source)).
					Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					cLogs, err := docker.Container.Logs.Execute(container.ID)
					Expect(err).NotTo(HaveOccurred())
					return cLogs.String()
				}).Should(
					ContainSubstring("Added 1 additional CA certificate(s) to system truststore"),
				)

				request, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%s", container.HostPort("8080")), nil)
				Expect(err).NotTo(HaveOccurred())

				var response *http.Response
				Eventually(func() error {
					var err error
					response, err = client.Do(request)
					return err
				}).Should(BeNil())
				defer response.Body.Close()

				Expect(response.StatusCode).To(Equal(http.StatusOK))

				content, err := ioutil.ReadAll(response.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(ContainSubstring("Hello world, Authenticated User!"))
			})
		})

	})
}
