---
title: "Documentation"
description: "How a bedrock tenant is built and how to deploy and run it."
cards:
  - title: "Understand bedrock"
    text: "Learn how a tenant is built, how its parts fit together and why it is designed that way."
    links:
      - { name: "What bedrock is", url: "/docs/concepts/overview/" }
      - { name: "Inside a tenant cluster", url: "/docs/concepts/tenant-cluster/" }
      - { name: "Security model", url: "/docs/concepts/security-model/" }
      - { name: "Failure model", url: "/docs/concepts/failure-model/" }
    button: { name: "View concepts", url: "/docs/concepts/" }
  - title: "Try bedrock"
    text: "Follow the tutorial to go from a set of empty machines to a hardened, verified tenant cluster."
    links:
      - { name: "Deploy your first tenant", url: "/docs/tutorials/first-tenant/" }
    button: { name: "View tutorials", url: "/docs/tutorials/" }
  - title: "Run a tenant"
    text: "Short guides for the tasks you do after the cluster is up."
    links:
      - { name: "Verify a deployment", url: "/docs/tasks/verify/" }
      - { name: "Reach the dashboards", url: "/docs/tasks/dashboards/" }
      - { name: "Turn on an event bus", url: "/docs/tasks/event-buses/" }
      - { name: "Allow a new image publisher", url: "/docs/tasks/image-publishers/" }
    button: { name: "View tasks", url: "/docs/tasks/" }
  - title: "Look something up"
    text: "Versions, namespaces, network flows, policies and alerts, in tables."
    links:
      - { name: "Components and versions", url: "/docs/reference/components/" }
      - { name: "Network flows", url: "/docs/reference/network-flows/" }
      - { name: "Admission policies", url: "/docs/reference/policies/" }
      - { name: "Alert rules", url: "/docs/reference/alerts/" }
    button: { name: "View reference", url: "/docs/reference/" }
---

bedrock is a Kubernetes cluster engine for production workloads. One command turns bare machines into a cluster with its own storage and databases and observability and policy and network. These docs describe that cluster: what runs in it and why and how to operate it.
