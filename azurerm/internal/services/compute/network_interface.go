package compute

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-09-01/network"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/azure"
)

type connectionInfo struct {
	// primaryPrivateAddress is the Primary Private IP Address for this VM
	primaryPrivateAddress string

	// privateAddresses is a slice of the Private IP Addresses supported by this VM
	privateAddresses []string

	// primaryPublicAddress is the Primary Public IP Address for this VM
	primaryPublicAddress string

	// publicAddresses is a slice of the Public IP Addresses supported by this VM
	publicAddresses []string
}

// retrieveConnectionInformation retrieves all of the Public and Private IP Addresses assigned to a Virtual Machine
func retrieveConnectionInformation(ctx context.Context, nicsClient *network.InterfacesClient, pipsClient *network.PublicIPAddressesClient, input *compute.VirtualMachineProperties) connectionInfo {
	if input == nil || input.NetworkProfile == nil || input.NetworkProfile.NetworkInterfaces == nil {
		return connectionInfo{}
	}

	privateIPAddresses := make([]string, 0)
	publicIPAddresses := make([]string, 0)

	if input != nil && input.NetworkProfile != nil && input.NetworkProfile.NetworkInterfaces != nil {
		for _, v := range *input.NetworkProfile.NetworkInterfaces {
			if v.ID == nil {
				continue
			}

			nic := retrieveIPAddressesForNIC(ctx, nicsClient, pipsClient, *v.ID)
			if nic == nil {
				continue
			}

			privateIPAddresses = append(privateIPAddresses, nic.privateIPAddresses...)
			publicIPAddresses = append(publicIPAddresses, nic.publicIPAddresses...)
		}
	}

	primaryPrivateAddress := ""
	if len(privateIPAddresses) > 0 {
		primaryPrivateAddress = privateIPAddresses[0]
	}
	primaryPublicAddress := ""
	if len(publicIPAddresses) > 0 {
		primaryPublicAddress = publicIPAddresses[0]
	}

	return connectionInfo{
		primaryPrivateAddress: primaryPrivateAddress,
		privateAddresses:      privateIPAddresses,
		primaryPublicAddress:  primaryPublicAddress,
		publicAddresses:       publicIPAddresses,
	}
}

type interfaceDetails struct {
	// privateIPAddresses is a slice of the Private IP Addresses supported by this VM
	privateIPAddresses []string

	// publicIPAddresses is a slice of the Public IP Addresses supported by this VM
	publicIPAddresses []string
}

// retrieveIPAddressesForNIC returns the Public and Private IP Addresses associated
// with the specified Network Interface
func retrieveIPAddressesForNIC(ctx context.Context, nicClient *network.InterfacesClient, pipClient *network.PublicIPAddressesClient, nicID string) *interfaceDetails {
	id, err := azure.ParseAzureResourceID(nicID)
	if err != nil {
		return nil
	}

	resourceGroup := id.ResourceGroup
	name := id.Path["networkInterfaces"]

	nic, err := nicClient.Get(ctx, resourceGroup, name, "")
	if err != nil {
		return nil
	}

	if nic.InterfacePropertiesFormat == nil || nic.InterfacePropertiesFormat.IPConfigurations == nil {
		return nil
	}

	privateIPAddresses := make([]string, 0)
	publicIPAddresses := make([]string, 0)
	for _, config := range *nic.InterfacePropertiesFormat.IPConfigurations {
		if props := config.InterfaceIPConfigurationPropertiesFormat; props != nil {

			if props.PrivateIPAddress != nil {
				privateIPAddresses = append(privateIPAddresses, *props.PrivateIPAddress)
			}

			if pip := props.PublicIPAddress; pip != nil {
				if pip.ID != nil {
					publicIPAddress, err := retrievePublicIPAddress(ctx, pipClient, *pip.ID)
					if err != nil {
						continue
					}

					if publicIPAddress != nil {
						publicIPAddresses = append(publicIPAddresses, *publicIPAddress)
					}
				}
			}
		}
	}

	return &interfaceDetails{
		privateIPAddresses: privateIPAddresses,
		publicIPAddresses:  publicIPAddresses,
	}
}

// retrievePublicIPAddress returns the Public IP Address associated with an Azure Public IP
func retrievePublicIPAddress(ctx context.Context, client *network.PublicIPAddressesClient, publicIPAddressID string) (*string, error) {
	id, err := azure.ParseAzureResourceID(publicIPAddressID)
	if err != nil {
		return nil, err
	}

	resourceGroup := id.ResourceGroup
	name := id.Path["publicIPAddresses"]

	pip, err := client.Get(ctx, resourceGroup, name, "")
	if err != nil {
		return nil, err
	}

	// NOTE: apparently there's a bug here with Dynamic Public IP's?
	if props := pip.PublicIPAddressPropertiesFormat; props != nil {
		return props.IPAddress, nil
	}

	return nil, nil
}

// setConnectionInformation sets the connection information required for Provisioners
// to connect to the Virtual Machine. A Public IP Address is used if one is available
// but this falls back to a Private IP Address (which should always exist)
func setConnectionInformation(d *schema.ResourceData, input connectionInfo, isWindows bool) {
	provisionerType := "ssh"
	if isWindows {
		provisionerType = "winrm"
	}

	ipAddress := input.primaryPublicAddress
	if ipAddress == "" {
		ipAddress = input.primaryPrivateAddress
	}

	d.SetConnInfo(map[string]string{
		"type": provisionerType,
		"host": ipAddress,
	})
}
