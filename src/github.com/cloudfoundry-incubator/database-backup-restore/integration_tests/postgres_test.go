// Copyright (C) 2017-Present Pivotal Software, Inc. All rights reserved.
//
// This program and the accompanying materials are made available under
// the terms of the under the Apache License, Version 2.0 (the "License”);
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//
// See the License for the specific language governing permissions and
// limitations under the License.

package integration_tests

import (
	"fmt"
	"os"
	"os/exec"

	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Postgres", func() {
	var session *gexec.Session
	var username = "testuser"
	var host = "127.0.0.1"
	var port = 1234
	var databaseName = "mycooldb"
	var password = "password"
	var artifactFile string
	var compiledSDKPath string
	var err error
	var configFile *os.File

	BeforeEach(func() {
		compiledSDKPath, err = gexec.Build(
			"github.com/cloudfoundry-incubator/database-backup-restore/cmd/database-backup-restore")
		Expect(err).NotTo(HaveOccurred())

		artifactFile = tempFilePath()

		fakePgClient.Reset()
		fakePgDump94.Reset()
		fakePgDump96.Reset()
		fakePgRestore94.Reset()
		fakePgRestore96.Reset()

		envVars["PG_CLIENT_PATH"] = fakePgClient.Path
		envVars["PG_DUMP_9_4_PATH"] = fakePgDump94.Path
		envVars["PG_DUMP_9_6_PATH"] = fakePgDump96.Path
		envVars["PG_RESTORE_9_4_PATH"] = fakePgRestore94.Path
		envVars["PG_RESTORE_9_6_PATH"] = fakePgRestore96.Path

		configFile = saveFile(fmt.Sprintf(`{
				"adapter":  "postgres",
				"username": "%s",
				"password": "%s",
				"host":     "%s",
				"port":     %d,
				"database": "%s"
			}`,
			username,
			password,
			host,
			port,
			databaseName))
	})

	Context("backup", func() {
		JustBeforeEach(func() {
			session = run(compiledSDKPath, envVars,
				"--artifact-file", artifactFile,
				"--config", configFile.Name(),
				"--backup",
			)
		})

		Context("Postgres database server is version 9.4", func() {
			BeforeEach(func() {
				fakePgClient.WhenCalled().WillPrintToStdOut(
					" PostgreSQL 9.4.9 on x86_64-unknown-linux-gnu, compiled by gcc " +
						"(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4, 64-bit").
					WillExitWith(0)
			})

			Context("when pg_dump succeeds", func() {
				BeforeEach(func() {
					fakePgDump94.WhenCalled().WillExitWith(0)
				})

				It("takes a backup", func() {
					By("getting the server version", func() {
						expectedArgs := []string{
							"--tuples-only",
							fmt.Sprintf("--username=%s", username),
							fmt.Sprintf("--host=%s", host),
							fmt.Sprintf("--port=%d", port),
							databaseName,
							`--command=SELECT VERSION()`,
						}

						Expect(fakePgClient.Invocations()).To(HaveLen(1))
						Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
					})

					By("dumping the database with the correct dump binary", func() {
						expectedArgs := []string{
							"--verbose",
							fmt.Sprintf("--username=%s", username),
							fmt.Sprintf("--host=%s", host),
							fmt.Sprintf("--port=%d", port),
							"--format=custom",
							fmt.Sprintf("--file=%s", artifactFile),
							databaseName,
						}

						Expect(fakePgDump94.Invocations()).To(HaveLen(1))
						Expect(fakePgDump94.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						Expect(fakePgDump94.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
					})

					By("not invoking the dump binary for a different version", func() {
						Expect(fakePgDump96.Invocations()).To(HaveLen(0))
					})

					Expect(session).Should(gexec.Exit(0))
				})

				Context("when 'tables' are specified in the configFile", func() {
					BeforeEach(func() {
						configFile = saveFile(fmt.Sprintf(`{
								"adapter":  "postgres",
								"username": "%s",
								"password": "%s",
								"host":     "%s",
								"port":     %d,
								"database": "%s",
								"tables": ["table1", "table2", "table3"]
							}`,
							username,
							password,
							host,
							port,
							databaseName))
						fakePgClient.WhenCalled().WillPrintToStdOut(
							" table1 \n table2 \n table3 \n\n\n").
							WillExitWith(0)
					})

					It("backs up the specified tables", func() {
						By("checking if the tables exist", func() {
							expectedArgs := []string{
								"--tuples-only",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								databaseName,
								`--command=SELECT table_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema='public';`,
							}

							Expect(fakePgClient.Invocations()).To(HaveLen(2))
							Expect(fakePgClient.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
							Expect(fakePgClient.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
						})

						By("calling pg_dump with the correct arguments", func() {
							expectedArgs := []string{
								"--verbose",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								"--format=custom",
								fmt.Sprintf("--file=%s", artifactFile),
								databaseName,
								"-t", "table1",
								"-t", "table2",
								"-t", "table3",
							}

							Expect(fakePgDump94.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						})

						Expect(session).Should(gexec.Exit(0))
					})
				})

				Context("when missing 'tables' are specified in the configFile", func() {
					BeforeEach(func() {
						configFile = saveFile(fmt.Sprintf(`{
								"adapter":  "postgres",
								"username": "%s",
								"password": "%s",
								"host":     "%s",
								"port":     %d,
								"database": "%s",
								"tables": ["table1", "table2", "table3"]
							}`,
							username,
							password,
							host,
							port,
							databaseName))
						fakePgClient.WhenCalled().WillPrintToStdOut(
							" table1 \n table2 \n\n\n").
							WillExitWith(0)
					})

					It("fails", func() {
						By("checking if the tables exist", func() {
							expectedArgs := []string{
								"--tuples-only",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								databaseName,
								`--command=SELECT table_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema='public';`,
							}

							Expect(fakePgClient.Invocations()).To(HaveLen(2))
							Expect(fakePgClient.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
							Expect(fakePgClient.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
						})

						By("exiting with a helpful error message", func() {
							Expect(session).Should(gexec.Exit(1))
							Expect(session.Err).Should(gbytes.Say(`can't find specified table\(s\): table3`))
						})
					})
				})

			})

			Context("when pg_dump fails", func() {
				BeforeEach(func() {
					configFile = saveFile(fmt.Sprintf(`{
								"adapter":  "postgres",
								"username": "%s",
								"password": "%s",
								"host":     "%s",
								"port":     %d,
								"database": "%s",
								"tables": ["table1", "table2", "table3"]
							}`,
						username,
						password,
						host,
						port,
						databaseName))
					fakePgClient.WhenCalled().WillPrintToStdOut(
						" table1 \n table2 \n\n\n").
						WillExitWith(0)
					fakePgDump94.WhenCalled().WillExitWith(1)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})

		})

		Context("Postgres database server is version 9.6", func() {
			BeforeEach(func() {
				fakePgClient.WhenCalled().WillPrintToStdOut(
					" PostgreSQL 9.6.3 on x86_64-pc-linux-gnu, compiled by gcc " + "" +
						"(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4, 64-bit").
					WillExitWith(0)

			})

			Context("when pg_dump succeeds", func() {
				BeforeEach(func() {
					fakePgDump96.WhenCalled().WillExitWith(0)
				})

				It("takes a backup", func() {
					By("getting the server version", func() {
						expectedArgs := []string{
							"--tuples-only",
							fmt.Sprintf("--username=%s", username),
							fmt.Sprintf("--host=%s", host),
							fmt.Sprintf("--port=%d", port),
							databaseName,
							`--command=SELECT VERSION()`,
						}

						Expect(fakePgClient.Invocations()).To(HaveLen(1))
						Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
					})

					By("dumping the database with the correct dump binary", func() {
						expectedArgs := []string{
							"--verbose",
							fmt.Sprintf("--username=%s", username),
							fmt.Sprintf("--host=%s", host),
							fmt.Sprintf("--port=%d", port),
							"--format=custom",
							fmt.Sprintf("--file=%s", artifactFile),
							databaseName,
						}

						Expect(fakePgDump96.Invocations()).To(HaveLen(1))
						Expect(fakePgDump96.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
					})

					By("not invoking the dump binary for a different version", func() {
						Expect(fakePgDump94.Invocations()).To(HaveLen(0))
					})

					Expect(session).Should(gexec.Exit(0))
				})

				Context("when 'tables' are specified in the configFile", func() {
					BeforeEach(func() {
						configFile = saveFile(fmt.Sprintf(`{
								"adapter":  "postgres",
								"username": "%s",
								"password": "%s",
								"host":     "%s",
								"port":     %d,
								"database": "%s",
								"tables": ["table1", "table2", "table3"]
							}`,
							username,
							password,
							host,
							port,
							databaseName))
						fakePgClient.WhenCalled().WillPrintToStdOut(
							" table1 \n table2 \n table3 \n\n\n").
							WillExitWith(0)
					})

					It("backs up the specified tables", func() {
						By("checking if the tables exist", func() {
							expectedArgs := []string{
								"--tuples-only",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								databaseName,
								`--command=SELECT table_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema='public';`,
							}

							Expect(fakePgClient.Invocations()).To(HaveLen(2))
							Expect(fakePgClient.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
							Expect(fakePgClient.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
						})

						By("calling pg_dump with the correct arguments", func() {
							expectedArgs := []string{
								"--verbose",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								"--format=custom",
								fmt.Sprintf("--file=%s", artifactFile),
								databaseName,
								"-t", "table1",
								"-t", "table2",
								"-t", "table3",
							}

							Expect(fakePgDump96.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))
						})

						Expect(session).Should(gexec.Exit(0))
					})
				})

				Context("when missing 'tables' are specified in the configFile", func() {
					BeforeEach(func() {
						configFile = saveFile(fmt.Sprintf(`{
								"adapter":  "postgres",
								"username": "%s",
								"password": "%s",
								"host":     "%s",
								"port":     %d,
								"database": "%s",
								"tables": ["table1", "table2", "table3"]
							}`,
							username,
							password,
							host,
							port,
							databaseName))
						fakePgClient.WhenCalled().WillPrintToStdOut(
							" table1 \n table2 \n\n\n").
							WillExitWith(0)
					})

					It("fails", func() {
						By("checking if the tables exist", func() {
							expectedArgs := []string{
								"--tuples-only",
								fmt.Sprintf("--username=%s", username),
								fmt.Sprintf("--host=%s", host),
								fmt.Sprintf("--port=%d", port),
								databaseName,
								`--command=SELECT table_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema='public';`,
							}

							Expect(fakePgClient.Invocations()).To(HaveLen(2))
							Expect(fakePgClient.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
							Expect(fakePgClient.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
						})

						By("exiting with a helpful error message", func() {
							Expect(session).Should(gexec.Exit(1))
							Expect(session.Err).Should(gbytes.Say(`can't find specified table\(s\): table3`))
						})
					})
				})

				Context("when TLS is configured", func() {
					Context("when skip host verify is not set", func() {
						BeforeEach(func() {
							configFile = saveFile(fmt.Sprintf(`{
							"adapter":  "postgres",
							"username": "%s",
							"password": "%s",
							"host":     "%s",
							"port":     %d,
							"database": "%s",
							"tls": {
								"cert": {
									"ca": "A_CA_CERT"
								}
							}
						}`,
								username,
								password,
								host,
								port,
								databaseName))
						})

						It("calls psql and pg_dump with the correct arguments", func() {
							By("calling psql to detect the version", func() {
								Expect(fakePgClient.Invocations()).To(HaveLen(1))
								Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(
									"--tuples-only",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									databaseName,
									`--command=SELECT VERSION()`,
								))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-full"))
							})

							By("then calling dump", func() {
								expectedArgs := []string{
									"--verbose",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									"--format=custom",
									fmt.Sprintf("--file=%s", artifactFile),
									databaseName,
								}

								Expect(fakePgDump96.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))

								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-full"))

								caCertPath := fakePgDump96.Invocations()[0].Env()["PGSSLROOTCERT"]
								Expect(caCertPath).To(BeAnExistingFile())
								Expect(ioutil.ReadFile(caCertPath)).To(Equal([]byte("A_CA_CERT")))
							})
						})
					})

					Context("when skip_host_verify is set to true", func() {
						BeforeEach(func() {
							configFile = saveFile(fmt.Sprintf(`{
							"adapter":  "postgres",
							"username": "%s",
							"password": "%s",
							"host":     "%s",
							"port":     %d,
							"database": "%s",
							"tls": {
								"skip_host_verify": true,
								"cert": {
									"ca": "A_CA_CERT"
								}
							}
						}`,
								username,
								password,
								host,
								port,
								databaseName))
						})

						It("calls psql and pg_dump with the correct arguments", func() {
							By("calling psql to detect the version", func() {
								Expect(fakePgClient.Invocations()).To(HaveLen(1))
								Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(
									"--tuples-only",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									databaseName,
									`--command=SELECT VERSION()`,
								))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-ca"))
							})

							By("then calling dump", func() {
								expectedArgs := []string{
									"--verbose",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									"--format=custom",
									fmt.Sprintf("--file=%s", artifactFile),
									databaseName,
								}

								Expect(fakePgDump96.Invocations()[0].Args()).Should(ConsistOf(expectedArgs))

								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgDump96.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-ca"))

								caCertPath := fakePgDump96.Invocations()[0].Env()["PGSSLROOTCERT"]
								Expect(caCertPath).To(BeAnExistingFile())
								Expect(ioutil.ReadFile(caCertPath)).To(Equal([]byte("A_CA_CERT")))
							})
						})
					})
				})
			})

			Context("and pg_dump fails", func() {
				BeforeEach(func() {
					fakePgDump96.WhenCalled().WillExitWith(1)

					fakePgClient.WhenCalled().WillPrintToStdOut(
						" PostgreSQL 9.6.3 on x86_64-pc-linux-gnu, compiled by gcc " + "" +
							"(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4, 64-bit").
						WillExitWith(0)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})
		})
	})

	Context("restore", func() {
		JustBeforeEach(func() {
			session = run(compiledSDKPath, envVars,
				"--artifact-file", artifactFile,
				"--config", configFile.Name(),
				"--restore",
			)
		})

		Context("Postgres database server is version 9.4", func() {
			BeforeEach(func() {
				fakePgClient.WhenCalled().WillPrintToStdOut(
					" PostgreSQL 9.4.9 on x86_64-unknown-linux-gnu, compiled by gcc " +
						"(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4, 64-bit").
					WillExitWith(0)
			})

			Context("pg_restore succeeds", func() {
				BeforeEach(func() {
					fakePgRestore94.WhenCalled().WillExitWith(0)
					fakePgRestore94.WhenCalled().WillExitWith(0)
				})

				It("calls pg_restore to get information about the restore", func() {
					Expect(fakePgRestore94.Invocations()).To(HaveLen(2))

					Expect(fakePgRestore94.Invocations()[0].Args()).To(Equal([]string{"--list", artifactFile}))

					expectedArgs := []interface{}{
						"--verbose",
						fmt.Sprintf("--username=%s", username),
						fmt.Sprintf("--host=%s", host),
						fmt.Sprintf("--port=%d", port),
						"--format=custom",
						fmt.Sprintf("--dbname=%s", databaseName),
						"--clean",
						HavePrefix("--use-list="),
						artifactFile,
					}

					Expect(fakePgRestore94.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
					Expect(fakePgRestore94.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
				})

				It("succeeds", func() {
					Expect(session).Should(gexec.Exit(0))
				})
			})

			Context("and pg_restore fails when restoring", func() {
				BeforeEach(func() {
					fakePgRestore94.WhenCalled().WillExitWith(0)
					fakePgRestore94.WhenCalled().WillExitWith(1)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})

			Context("and pg_restore fails to get file list", func() {
				BeforeEach(func() {
					fakePgRestore94.WhenCalled().WillExitWith(1)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})
		})

		Context("Postgres database server is version 9.6", func() {
			BeforeEach(func() {
				fakePgClient.WhenCalled().WillPrintToStdOut(
					" PostgreSQL 9.6.3 on x86_64-pc-linux-gnu, compiled by gcc " + "" +
						"(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4, 64-bit").
					WillExitWith(0)
			})

			Context("pg_restore succeeds", func() {
				BeforeEach(func() {
					fakePgRestore96.WhenCalled().WillExitWith(0)
					fakePgRestore96.WhenCalled().WillExitWith(0)
				})

				Context("when TLS block is not configured", func() {
					It("calls pg_restore to get information about the restore", func() {
						Expect(fakePgRestore96.Invocations()).To(HaveLen(2))

						Expect(fakePgRestore96.Invocations()[0].Args()).To(Equal([]string{"--list", artifactFile}))

						expectedArgs := []interface{}{
							"--verbose",
							fmt.Sprintf("--username=%s", username),
							fmt.Sprintf("--host=%s", host),
							fmt.Sprintf("--port=%d", port),
							"--format=custom",
							fmt.Sprintf("--dbname=%s", databaseName),
							"--clean",
							HavePrefix("--use-list="),
							artifactFile,
						}

						Expect(fakePgRestore96.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))
						Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
					})

					It("succeeds", func() {
						Expect(session).Should(gexec.Exit(0))
					})
				})

				Context("when TLS is configured", func() {
					Context("when skip_host_verify is not specified", func() {
						BeforeEach(func() {
							configFile = saveFile(fmt.Sprintf(`{
							"adapter":  "postgres",
							"username": "%s",
							"password": "%s",
							"host":     "%s",
							"port":     %d,
							"database": "%s",
							"tls": {
								"cert": {
									"ca": "A_CA_CERT"
								}
							}
						}`,
								username,
								password,
								host,
								port,
								databaseName))
						})

						It("calls psql and pg_restore with the correct arguments", func() {
							By("calling psql to detect the version", func() {
								Expect(fakePgClient.Invocations()).To(HaveLen(1))
								Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(
									"--tuples-only",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									databaseName,
									`--command=SELECT VERSION()`,
								))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-full"))
							})

							By("then calling pg_restore to restore", func() {
								Expect(fakePgRestore96.Invocations()).To(HaveLen(2))

								Expect(fakePgRestore96.Invocations()[0].Args()).To(Equal([]string{"--list", artifactFile}))

								expectedArgs := []interface{}{
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									"--verbose",
									"--format=custom",
									fmt.Sprintf("--dbname=%s", databaseName),
									"--clean",
									HavePrefix("--use-list="),
									artifactFile,
								}

								Expect(fakePgRestore96.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))

								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-full"))

								caCertPath := fakePgRestore96.Invocations()[1].Env()["PGSSLROOTCERT"]
								Expect(caCertPath).To(BeAnExistingFile())
								Expect(ioutil.ReadFile(caCertPath)).To(Equal([]byte("A_CA_CERT")))
							})
						})
					})

					Context("when skip_host_verify is set to true", func() {
						BeforeEach(func() {
							configFile = saveFile(fmt.Sprintf(`{
							"adapter":  "postgres",
							"username": "%s",
							"password": "%s",
							"host":     "%s",
							"port":     %d,
							"database": "%s",
							"tls": {
								"skip_host_verify": true,
								"cert": {
									"ca": "A_CA_CERT"
								}
							}
						}`,
								username,
								password,
								host,
								port,
								databaseName))
						})

						It("calls psql and pg_restore with the correct arguments", func() {
							By("calling psql to detect the version", func() {
								Expect(fakePgClient.Invocations()).To(HaveLen(1))
								Expect(fakePgClient.Invocations()[0].Args()).Should(ConsistOf(
									"--tuples-only",
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									databaseName,
									`--command=SELECT VERSION()`,
								))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgClient.Invocations()[0].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-ca"))
							})

							By("then calling pg_restore to restore", func() {
								Expect(fakePgRestore96.Invocations()).To(HaveLen(2))

								Expect(fakePgRestore96.Invocations()[0].Args()).To(Equal([]string{"--list", artifactFile}))

								expectedArgs := []interface{}{
									fmt.Sprintf("--username=%s", username),
									fmt.Sprintf("--host=%s", host),
									fmt.Sprintf("--port=%d", port),
									"--verbose",
									"--format=custom",
									fmt.Sprintf("--dbname=%s", databaseName),
									"--clean",
									HavePrefix("--use-list="),
									artifactFile,
								}

								Expect(fakePgRestore96.Invocations()[1].Args()).Should(ConsistOf(expectedArgs))

								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKeyWithValue("PGPASSWORD", password))
								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKey("PGSSLROOTCERT"))
								Expect(fakePgRestore96.Invocations()[1].Env()).Should(HaveKeyWithValue("PGSSLMODE", "verify-ca"))

								caCertPath := fakePgRestore96.Invocations()[1].Env()["PGSSLROOTCERT"]
								Expect(caCertPath).To(BeAnExistingFile())
								Expect(ioutil.ReadFile(caCertPath)).To(Equal([]byte("A_CA_CERT")))
							})
						})
					})
				})
			})

			Context("and pg_restore fails when restoring", func() {
				BeforeEach(func() {
					fakePgRestore96.WhenCalled().WillExitWith(0)
					fakePgRestore96.WhenCalled().WillExitWith(1)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})

			Context("and pg_restore fails to get file list", func() {
				BeforeEach(func() {
					fakePgRestore96.WhenCalled().WillExitWith(1)
				})

				It("also fails", func() {
					Eventually(session).Should(gexec.Exit(1))
				})
			})
		})
	})
})

func run(path string, env map[string]string, args ...string) *gexec.Session {
	cmd := exec.Command(path, args...)
	for key, val := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, val))
	}
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).ToNot(HaveOccurred())
	Eventually(session).Should(gexec.Exit())

	return session
}
