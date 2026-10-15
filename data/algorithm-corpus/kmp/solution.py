s=input().strip(); p=input().strip(); pi=[0]*len(p); j=0
for i in range(1,len(p)):
    while j and p[i]!=p[j]: j=pi[j-1]
    if p[i]==p[j]: j+=1
    pi[i]=j
j=0; ans=-1
for i,c in enumerate(s):
    while j and c!=p[j]: j=pi[j-1]
    if c==p[j]: j+=1
    if j==len(p): ans=i-len(p)+2; break
print(ans)
