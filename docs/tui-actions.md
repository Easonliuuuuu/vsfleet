# Detail actions and SSH

Open a row with `Enter`, then use `↑`/`↓` to select its header or a populated
field. `Enter` runs a single action or opens a menu when several apply.

## Detail pane actions

| Selected line | Actions |
|---|---|
| VM or host header | SSH, open vSphere/Host Client, copy managed object reference |
| VM recognized as a vCenter | Add vCenter context, or switch to its saved context |
| VM IP or DNS name | SSH, copy the SSH command or the value |
| Host, datastore, network, or cluster header | Show its VMs |
| Datastore header | [Browse files or Find in datastore](tui-storage.md) |
| VM Host or Cluster field | Jump to that resource's row |
| Cluster Hosts, Host states, or VMs field | Show hosts or VMs in the cluster |
| Cluster Datastores field | Open [Storage](tui-network.md#cluster-workspace) |
| Other fields | Copy value |

Menus skip disabled actions while navigating and list them last with a reason.
Inapplicable actions are omitted. Proxied vCenters disable browser actions
because the browser has no route. `vsfleet demo` disables process and browser
launches.

A VM qualifies as a vCenter when a component of its name or guest DNS name
starts with `vcsa`, `vcenter`, or `vcentre`, or equals `vc` or `vc` plus digits.
A saved nested context also qualifies. Add other vCenters with
`vsfleet config set-context` or the [Contexts screen](tui.md#contexts-and-vapps).

“Show VMs on this …” keeps the table scoped until `Esc` clears that scope
before clearing anything else.

## SSH

<span id="ssh-destinations-and-settings"></span>
<span id="openssh-alias-discovery"></span>
<span id="destination-and-route"></span>

A VM's header offers one **SSH to …** entry per address it reports: its guest
DNS name, its primary IP, and, when viewing a capture, every other address on
its adapters, labelled with the adapter's network. A host offers its
registered name.

vsfleet runs plain `ssh <address>` and adds nothing. vSphere tells it which
addresses a machine has, not which one your workstation can reach or how, so
the user, key, port, timeout and any jump host come from `~/.ssh/config`:

```text
Host 172.31.7.*
    User devops
    ProxyJump bastion.example.internal

Host *.corp.local
    User ubuntu
```

A failed session shows OpenSSH's last error line in the footer.
