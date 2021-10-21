# UPP - Generic Read/Write Aurora

Reads and Writes records for PAC draft content and annotations to the PAC Aurora DB cluster.

## Code

generic-rw-aurora

## Primary URL

<https://upp-prod-delivery-glb.upp.ft.com/__generic-rw-aurora/>

## Service Tier

Platinum

## Lifecycle Stage

Production

## Host Platform

AWS

## Architecture

Aurora MySQL DB is used for storing the annotation data. There is one write and one read instance in each of the EU and 
US zones, however all the reads and writes go only to one of the clusters (the principal).

## Contains Personal Data

No

## Contains Sensitive Data

No

## Failover Architecture Type

ActiveActive

## Failover Process Type

FullyAutomated

## Failback Process Type

FullyAutomated

## Failover Details

The service is PAC cluster.
The failover guide for the cluster is located here:
<https://github.com/Financial-Times/upp-docs/tree/master/failover-guides/pac-cluster>

## Data Recovery Process Type

NotApplicable

## Data Recovery Details

The service does not store data, so it does not require any data recovery steps.

## Release Process Type

PartiallyAutomated

## Rollback Process Type

Manual

## Release Details

Manual failover is needed when a new version of
the service is deployed to production.
Otherwise, an automated failover is going to take place when releasing.
For more details about the failover process please see: <https://github.com/Financial-Times/upp-docs/tree/master/failover-guides/pac-cluster>

## Key Management Process Type

Manual

## Key Management Details

To access the service clients need to provide basic auth credentials.
To rotate credentials you need to login to a particular cluster and update varnish-auth secrets.

## Monitoring

Service in UPP K8S PAC clusters:

- PAC-Prod-EU health: <https://pac-prod-eu.ft.com/__health/__pods-health?service-name=generic-rw-aurora>
- PAC-Prod-US health: <https://pac-prod-us.ft.com/__health/__pods-health?service-name=generic-rw-aurora>

## First Line Troubleshooting

<https://github.com/Financial-Times/upp-docs/tree/master/guides/ops/first-line-troubleshooting>

## Second Line Troubleshooting

Please refer to the GitHub repository README for troubleshooting information.
