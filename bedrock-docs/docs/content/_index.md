---
title: "bedrock documentation"
description: "Self-hosted Kubernetes platform for managed service providers. One hardened cluster per tenant, nothing shared."
cards:
  - title: "Understand bedrock"
    text: "Learn how a tenant is built, how its parts fit together and why it is designed that way."
    links:
      - { name: "What bedrock is", url: "/concepts/overview/" }
      - { name: "The tier model", url: "/concepts/tiers/" }
      - { name: "Inside a tenant cluster", url: "/concepts/tenant-cluster/" }
      - { name: "Security model", url: "/concepts/security-model/" }
      - { name: "Failure model", url: "/concepts/failure-model/" }
    button: { name: "View concepts", url: "/concepts/" }
  - title: "Try bedrock"
    text: "Follow the tutorial to go from three empty machines to a hardened, verified tenant cluster."
    links:
      - { name: "Deploy your first tenant", url: "/tutorials/first-tenant/" }
    button: { name: "View tutorials", url: "/tutorials/" }
  - title: "Run a tenant"
    text: "Short guides for the tasks you do after the cluster is up."
    links:
      - { name: "Verify a deployment", url: "/tasks/verify/" }
      - { name: "Reach the dashboards", url: "/tasks/dashboards/" }
      - { name: "Turn on an event bus", url: "/tasks/event-buses/" }
      - { name: "Allow a new image publisher", url: "/tasks/image-publishers/" }
    button: { name: "View tasks", url: "/tasks/" }
  - title: "Look something up"
    text: "Versions, namespaces, network flows, policies and alerts, in tables."
    links:
      - { name: "Components and versions", url: "/reference/components/" }
      - { name: "Network flows", url: "/reference/network-flows/" }
      - { name: "Admission policies", url: "/reference/policies/" }
      - { name: "Alert rules", url: "/reference/alerts/" }
    button: { name: "View reference", url: "/reference/" }
---

bedrock turns plain machines into isolated, hardened Kubernetes tenants. Each tenant gets its own cluster on its own nodes, with its own storage, databases, observability, policy and network. Today the tenant layer is built and tested. The layers that create and manage tenants are on the [roadmap](roadmap/).
