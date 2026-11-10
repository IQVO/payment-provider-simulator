import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/rest/payment-provider-simulator-api",
    },
    {
      type: "category",
      label: "Tokens",
      link: {
        type: "doc",
        id: "api-reference/rest/tokens",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/issue-token",
          label: "Issue a payment-method token for a test scenario",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Authorizations",
      link: {
        type: "doc",
        id: "api-reference/rest/authorizations",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/authorize",
          label: "Place a hold on the payment method a token stands for",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/get-authorization",
          label: "Read a hold with its captures and refunds",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/void-authorization",
          label: "Release the whole remaining hold",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/reauthorize",
          label: "Replace the hold by a fresh one of the given amount",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/reduce-authorization",
          label: "Lower the remaining hold (partial release)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/capture",
          label: "Take money from the hold",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api-reference/rest/refund",
          label: "Return captured money",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Platform",
      link: {
        type: "doc",
        id: "api-reference/rest/platform",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/healthz",
          label: "Liveness",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/readyz",
          label: "Readiness",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
