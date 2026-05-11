import React, { useState } from 'react';

interface BrandLogoProps {
  alt: string;
  className?: string;
}

const BrandLogo: React.FC<BrandLogoProps> = ({ alt, className = '' }) => {
  const [hasImageError, setHasImageError] = useState(false);

  if (!hasImageError) {
    return (
      <img
        src="/gtmanager-logo.png"
        alt={alt}
        className={className}
        onError={() => setHasImageError(true)}
      />
    );
  }

  return (
    <span
      role="img"
      aria-label={alt}
      className={`inline-flex shrink-0 items-center justify-center rounded-xl bg-[linear-gradient(135deg,#1677FF_0%,#0958D9_100%)] text-[11px] font-black leading-none tracking-normal text-white shadow-[0_12px_24px_-18px_rgba(22,119,255,0.75)] ${className}`}
    >
      GT
    </span>
  );
};

export default BrandLogo;
