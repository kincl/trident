###################
Element (SolidFire)
###################

To create and use a SolidFire backend, you will need:

* A :ref:`supported SolidFire storage system <Supported backends (storage)>`
* Complete `SolidFire backend preparation`_
* Credentials to a SolidFire cluster admin or tenant user that can manage volumes

.. _SolidFire backend preparation:

Preparation
-----------

All of your Kubernetes worker nodes must have the appropriate iSCSI tools
installed. See the :ref:`worker configuration guide <iSCSI>` for more details.

If you're using CHAP (``UseCHAP`` is *true*), no further preparation is
required. It is recommended to explicitly set the ``UseCHAP`` option to use CHAP.
Otherwise, see the :ref:`access groups guide <Using access groups>` below.

If neither ``AccessGroups`` or ``UseCHAP`` are set then one of the following
rules applies:
* If the default ``trident`` access group is detected then access groups are used.
* If no access group is detected and Kubernetes version >= 1.7 then CHAP is used.


Backend configuration options
-----------------------------

================== =============================================================== ================================================
Parameter          Description                                                     Default
================== =============================================================== ================================================
version            Always 1
storageDriverName  Always "solidfire-san"
backendName        Custom name for the storage backend                             "solidfire\_" + storage (iSCSI) IP address
Endpoint           MVIP for the SolidFire cluster with tenant credentials
SVIP               Storage (iSCSI) IP address and port
TenantName         Tenant name to use (created if not found)
InitiatorIFace     Restrict iSCSI traffic to a specific host interface             "default"
UseCHAP            Use CHAP to authenticate iSCSI
AccessGroups       List of Access Group IDs to use                                 Finds the ID of an access group named "trident"
Types              QoS specifications (see below)
limitVolumeSize    Fail provisioning if requested volume size is above this value  "" (not enforced by default)
================== =============================================================== ================================================

Example configuration
---------------------

**Example 1 -  Backend configuration for solidfire-san driver with three volume types**

This example shows a backend file using CHAP authentication and modeling three volume types
with specific QoS guarantees. Most likely you would then define storage classes
to consume each of these using the ``IOPS`` storage class parameter.

.. code-block:: json

  {
      "version": 1,
      "storageDriverName": "solidfire-san",
      "Endpoint": "https://<user>:<password>@<mvip>/json-rpc/8.0",
      "SVIP": "<svip>:3260",
      "TenantName": "<tenant>",
      "UseCHAP": true,
      "Types": [{"Type": "Bronze", "Qos": {"minIOPS": 1000, "maxIOPS": 2000, "burstIOPS": 4000}},
                {"Type": "Silver", "Qos": {"minIOPS": 4000, "maxIOPS": 6000, "burstIOPS": 8000}},
                {"Type": "Gold", "Qos": {"minIOPS": 6000, "maxIOPS": 8000, "burstIOPS": 10000}}]
  }

**Example 2 - Backend and storage class configuration for solidfire-san driver with virtual storage pools**

This example shows the backend definition file configured with virtual storage pools along with StorageClasses that refer back to them.

In the sample backend definition file shown below, specific defaults are set for all storage pools, which set the ``type`` at Silver. The virtual storage pools are defined in the ``storage`` section. In this example, some of the storage pool sets their own ``type``, and some pools overwrite the default values set above.

.. code-block:: json

  {
      "version": 1,
      "storageDriverName": "solidfire-san",
      "Endpoint": "https://<user>:<password>@<mvip>/json-rpc/8.0",
      "SVIP": "<svip>:3260",
      "TenantName": "<tenant>",
      "UseCHAP": true,
      "Types": [{"Type": "Bronze", "Qos": {"minIOPS": 1000, "maxIOPS": 2000, "burstIOPS": 4000}},
                {"Type": "Silver", "Qos": {"minIOPS": 4000, "maxIOPS": 6000, "burstIOPS": 8000}},
                {"Type": "Gold", "Qos": {"minIOPS": 6000, "maxIOPS": 8000, "burstIOPS": 10000}}],

      "defaults": {
            "type": "Silver"
      },

      "labels":{"store":"solidfire"},
      "region": "us-east-1",

      "storage": [
          {
              "labels":{"performance":"gold", "cost":"4"},
              "zone":"us-east-1a",
              "type":"Gold"
          },
          {
              "labels":{"performance":"silver", "cost":"3"},
              "zone":"us-east-1b",
              "type":"Silver"
          },
          {
              "labels":{"performance":"bronze", "cost":"2"},
              "zone":"us-east-1c",
              "type":"Bronze"
          },
          {
              "labels":{"performance":"silver", "cost":"1"},
              "zone":"us-east-1d"
          }
      ]
  }

The following StorageClass definitions refer to the above virtual storage pools. Using the ``parameters.selector`` field, each StorageClass calls out which virtual pool(s) may be used to host a volume. The volume will have the aspects defined in the chosen virtual pool.

The first StorageClass (``solidfire-gold-four``) will map to the first virtual storage pool. This is the only pool offering offering gold performance with a ``Volume Type QoS`` of Gold. The last StorageClass (``solidfire-silver``) calls out any storage pool which offers a silver performance. Trident will decide which virtual storage pool is selected and will ensure the storage requirement is met.

.. code-block:: yaml

    apiVersion: storage.k8s.io/v1
    kind: StorageClass
    metadata:
      name: solidfire-gold-four
    provisioner: netapp.io/trident
    parameters:
      selector: "performance=gold; cost=4"
    ---
    apiVersion: storage.k8s.io/v1
    kind: StorageClass
    metadata:
      name: solidfire-silver-three
    provisioner: netapp.io/trident
    parameters:
      selector: "performance=silver; cost=3"
    ---
    apiVersion: storage.k8s.io/v1
    kind: StorageClass
    metadata:
      name: solidfire-bronze-two
    provisioner: netapp.io/trident
    parameters:
      selector: "performance=bronze; cost=2"
    ---
    apiVersion: storage.k8s.io/v1
    kind: StorageClass
    metadata:
      name: solidfire-silver-one
    provisioner: netapp.io/trident
    parameters:
      selector: "performance=silver; cost=1"
    ---
    apiVersion: storage.k8s.io/v1
    kind: StorageClass
    metadata:
      name: solidfire-silver
    provisioner: netapp.io/trident
    parameters:
      selector: "performance=silver"


Using access groups
-------------------

.. note::
  Ignore this section if you are using CHAP, which we recommend to simplify
  management and avoid the scaling limit described below.

Trident can use volume access groups to control access to the volumes that it
provisions. If CHAP is disabled it expects to find an access group called
``trident`` unless one or more access group IDs are specified in the
configuration.

While Trident associates new volumes with the configured access group(s), it
does not create or otherwise manage access groups themselves. The access
group(s) must exist before the storage backend is added to Trident, and they
need to contain the iSCSI IQNs from every node in the Kubernetes cluster that
could potentially mount the volumes provisioned by that backend. In most
installations that's every worker node in the cluster.

For Kubernetes clusters with more than 64 nodes, you will need to use multiple
access groups. Each access group may contain up to 64 IQNs, and each volume can
belong to 4 access groups. With the maximum 4 access groups configured, any
node in a cluster up to 256 nodes in size will be able to access any volume.

If you're modifying the configuration from one that is using the default
``trident`` access group to one that uses others as well, include the ID for
the ``trident`` access group in the list.
