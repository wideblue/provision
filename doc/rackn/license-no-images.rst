.. Copyright (c) 2018 RackN Inc.
.. Licensed under the Apache License, Version 2.0 (the "License");
.. Digital Rebar Provision documentation under Digital Rebar master license
.. index::
  pair: Digital Rebar Provision; RackN Licensing

.. _rackn_licensing:

RackN Licensing Overview
~~~~~~~~~~~~~~~~~~~~~~~~

This document outlines the RackN limited use and commercial licensing information and initial setup steps necessary to access license entitlements.  If you have any questions or concerns, please feel free to contact us on Slack, or email us at support@rackn.com. 

*Limited Use licensing* of RackN Digital Rebar Platform is provided for individual users, trial and non-commercial teams.  There are
two models for this limited use license: embedded and online.

The embedded license is built into the platform and has restricted
entitlements for number of machines (20), contexts (3), pools (1)
and plugins (cannot run RackN authored plugins).

The online license is generated via the process below. Self-service
licenses start at 20 machines with 90-day self-service renewal.  They allow access to all publically available plugins in the RackN catalog.  Contact the RackN solution team if you would like to expand your entitlements.

*Commercial Use licensing* of RackN Digital Rebar Platform is
provided to Organizations.  License entitlements are enabled by
endpoint, unit counts or named modules.  The RackN solution team will need to setup an Organization with the correct license entitlements
for this type of license.

.. _rackn_licensing_prereqs:

Prerequisites
-------------

Here is a list of the necessary prerequisites that will need to be in place prior to you successfully using any licensed component(s):

#. You must have a Web Portal user account that is registered and functioning (sign up if you do not already have one here: https://portal.rackn.io/#/user/signup)
#. A functioning DRP Endpoint that is managable via the Web Portal

Insure you are logged in to the Rackn Web Portal (using the upper right "login" button).

Log in to the DRP Endpoint - which will be the username/password authentication dialog in the center of the Web Portal if you are not logged in. If you have not changed the default username and password, click the "Defaults" button, then "Login".


.. _rackn_licensing_overview:

Overview of Steps
-----------------

The following are the basic steps you need to perform to generate, enable, and use licensed plugins and contents.

1. Generate a License
2. Enable DRP Endpoints to use Licensed Content
4. Install Licensed Catalog Items

.. _rackn_licensing_generate_license:

Generate a License
------------------

The first time that you use a license entitlement, you will need to generate a license.  This creates the and starts the license entitlements based on the terms and conditions of your license (content, plugins, duration of license contract, etc.).  You will need to perform this step only once for each Organization that you manage that has a license entitlement. 

1. Visit the "License Management" page
1. In the "Online Activation" panel
   1. Signup for an account (one per organization)
   1. Login to the RackN Portal
   1. Click "Authorize" from the "License" Tab
1. Go to the "License Management" panel
   1. Verify that you see a green check mark
   1. Check your entitlements

.. _rackn_licensing_verify:

Verify Your License Entitlements
--------------------------------

The "License Management" page will show an overview of the licensed Contents, Features, and Plugin Providers that the current organization is entitled to.  Please verify you are using the correct Organization to view the licensing rights for that Organization (upper left blue pull down menu item).  If you are currently in the context of your personal Portal account (eg. it shows your email address or account), you will NOT be able to view or manage license entitlements.

Additionally, you can view each individual components entitlements from the overview license page.

2. Select "License Management"
3. Click in the "License Management" panel to the right
4. General license terms will be shown first
5. Each licensed component (feature, content, or plugin provider) will have individual licensing terms and details following the "General" terms

The General terms (soft and hard expire dates) will override each individual license expiration terms.  

"Soft" expire is when initial warning messages about subsequent de-licensing of a given feature will occur.

"Hard" expire is the date at which a given featre or term expires and will no longer be active.

.. _rackn_licensing_api_upgrade:

Check or Update an Existing License
------------------------------------

These steps require that you already have a valid RackN license.
The information contained in the license is used to verify your
entitlements and to authorize an updated license.  It relies on
online RackN License Management APIs.

To update manually, visit the UX _License Management_ page.
Click the "Check and Update License" button in the top right
corner of the "License Management" panel.  This uses the API
described below to update your license including adding new
endpoints.

To update automatically using the APIs, you must make the
a GET call with the required rackn headers.  If successful,
the call will return the latest valid license.  If a new
license is required, it will be automatically generated.

The most required fields are all avilable in the `sections.profiles.Params`
section of the License JSON file.
  * `rackn-ownerid` = `[base].rackn/license-object.OwnerId`
  * `rackn-contactid` = `[base].rackn/license-object.ContactId`
  * `rackn-key` = `[base].rackn/license`
  * `rackn-version` = `[base].rackn/license-object.Version`

The URL for the GET call is subject to change!  The current
(Nov 2019) URL is `https://1p0q9a8qob.execute-api.us-west-2.amazonaws.com/v40/license`

For faster performance, you can also use `https://1p0q9a8qob.execute-api.us-west-2.amazonaws.com/v40/check`
with the same headers to validate the license before asking for
updates.

Required Header Fields:
  * `rackn-ownerid`: license ownerid / org [or 'unknown']
  * `rackn-contactid`: license contactid / cognitor userid [or 'unknown']
  * `rackn-endpointid`: digital rebar endpoint id [or 'unknown']
  * `rackn-key`: license key [or 'unknown']
  * `rackn-version`: license version [or 'unknown']

The `rackn-endpointid` is the endpoint id (aka `drpid`) of the
Digital Rebar Provision endpoint to be licensed.  Licenses are
issued per endpoint.  You can add endpoints to a license by
sending a new endpoint with license information validated for
a different endpoint.  This will create a new license that can
be applied too all endpoints.

With header values exported, an example CURL call would resemble:

  ::
    curl GET -H "rackn-contactid: $CONTACTID" \
      -H "rackn-ownerid: $OWNERID" \
      -H "rackn-endpointid: $ENDPOINTID" \
      -H "rackn-key: $KEY" \
      -H "rackn-version: $VERSION" \
      https://1p0q9a8qob.execute-api.us-west-2.amazonaws.com/v40/license
