apt install -y open-iscsi nfs-common
systemctl enable --now iscsid
# run this on each node
