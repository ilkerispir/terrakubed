package client

import (
	"encoding/json"
	"fmt"
	"log"
)

const searchOrganizationModuleVersionQuery = `
{
  organization(filter: "name==%s") {
    edges {
      node {
        id
        name
        module(filter: "name==%s;provider==%s") {
            edges{
                node{
                    id
                    name
                    provider
                    version {
                        edges{
                            node{
                                id
                                version
                            }
                        }
                    }
                }
            }
        }
      }
    }
  }
}`

type SearchResponse struct {
	Organization struct {
		Edges []struct {
			Node struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Module struct {
					Edges []struct {
						Node struct {
							ID       string `json:"id"`
							Name     string `json:"name"`
							Provider string `json:"provider"`
							Version  struct {
								Edges []struct {
									Node struct {
										ID      string `json:"id"`
										Version string `json:"version"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"version"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"module"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"organization"`
}

func (c *Client) GetModuleVersions(organization, module, provider string) ([]string, error) {
	query := fmt.Sprintf(searchOrganizationModuleVersionQuery, organization, module, provider)

	respData, err := c.ExecuteQuery(query, nil)
	if err != nil {
		return nil, err
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(respData, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var versions []string
	for _, orgEdge := range searchResp.Organization.Edges {
		for _, modEdge := range orgEdge.Node.Module.Edges {
			for _, verEdge := range modEdge.Node.Version.Edges {
				versions = append(versions, verEdge.Node.Version)
			}
		}
	}

	log.Printf("Found %d versions for %s/%s", len(versions), module, provider)
	return versions, nil
}

const getModuleQuery = `
{
  organization(filter: "name==%s") {
    edges {
      node {
        id
        module(filter: "name==%s;provider==%s") {
            edges{
                node{
                    id
                    source 
                    folder
                    tagPrefix
                    vcs {
                        edges {
                            node {
                                id
                                clientId
                            }
                        }
                    }
                    ssh {
                        edges {
                            node {
                                sshType
                                privateKey
                            }
                        }
                    }
                }
            }
        }
      }
    }
  }
}`

type ModuleDetails struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Folder    string `json:"folder"`
	TagPrefix string `json:"tagPrefix"`
	Vcs       *struct {
		Edges []struct {
			Node struct {
				ID       string `json:"id"`
				VcsType  string `json:"vcsType"`
				ClientID string `json:"clientId"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"vcs"`
	Ssh *struct {
		Edges []struct {
			Node struct {
				SshType    string `json:"sshType"`
				PrivateKey string `json:"privateKey"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"ssh"`
}

type ModuleResponse struct {
	Organization struct {
		Edges []struct {
			Node struct {
				ID     string `json:"id"`
				Module struct {
					Edges []struct {
						Node ModuleDetails `json:"node"`
					} `json:"edges"`
				} `json:"module"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"organization"`
}

func (c *Client) GetModule(organization, module, provider string) (*ModuleDetails, string, error) {
	query := fmt.Sprintf(getModuleQuery, organization, module, provider)

	respData, err := c.ExecuteQuery(query, nil)
	if err != nil {
		return nil, "", err
	}

	var modResp ModuleResponse
	if err := json.Unmarshal(respData, &modResp); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Navigate to the module node
	for _, orgEdge := range modResp.Organization.Edges {
		for _, modEdge := range orgEdge.Node.Module.Edges {
			return &modEdge.Node, orgEdge.Node.ID, nil
		}
	}

	return nil, "", fmt.Errorf("module not found")
}
