import { use } from "react";
import { ServiceClientView } from "./client";

export function generateStaticParams() {
  return [{ name: 'api-gateway' }, { name: 'order-service' }, { name: 'payment-service' }];
}

type ParamsType = { name: string };
export default function ServicePage({ params }: { params: Promise<ParamsType> }) {
  const unwrappedParams = use(params);
  return <ServiceClientView name={unwrappedParams.name} />;
}